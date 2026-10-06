package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

var _ store.Reports = (*Store)(nil)

// applicationPrefix starts the application_name of every session, followed
// by the host name. Leader reads it back.
const applicationPrefix = "forgejo-github-bridge@"

// read runs fn in a read-only transaction: the console reads, and PostgreSQL
// rather than convention makes sure of it.
func (s *Store) read(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Leader implements store.Reports. The lock is a pg_try_advisory_lock on a
// bigint, which pg_locks shows split into two oids.
func (s *Store) Leader(ctx context.Context) (store.Leader, error) {
	var l store.Leader
	err := s.read(ctx, func(tx pgx.Tx) error {
		var name *string
		var connected *time.Time
		err := tx.QueryRow(ctx, `
			SELECT a.application_name, a.backend_start
			FROM pg_locks l LEFT JOIN pg_stat_activity a ON a.pid = l.pid
			WHERE l.locktype = 'advisory' AND l.granted AND l.objsubid = 1
			  AND l.classid::bigint = $1 AND l.objid::bigint = $2
			LIMIT 1`,
			int64(uint64(leaderLockID)>>32), int64(uint64(leaderLockID)&0xffffffff)).Scan(&name, &connected)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		l.Present = true
		if name != nil {
			l.Instance = strings.TrimPrefix(*name, applicationPrefix)
		}
		if connected != nil {
			l.ConnectedAt = *connected
		}
		return nil
	})
	return l, err
}

// QueueSummary implements store.Reports: DirtyStats, read-only.
func (s *Store) QueueSummary(ctx context.Context) (store.QueueStats, error) {
	var st store.QueueStats
	err := s.read(ctx, func(tx pgx.Tx) error {
		var oldest *time.Time
		err := tx.QueryRow(ctx,
			`SELECT count(*), count(*) FILTER (WHERE attempts > 0), min(marked_at) FROM dirty`).Scan(&st.Depth, &st.Failing, &oldest)
		if err == nil && oldest != nil {
			st.Oldest = *oldest
		}
		return err
	})
	return st, err
}

// RepositorySummaries implements store.Reports.
func (s *Store) RepositorySummaries(ctx context.Context) (map[string]store.RepositorySummary, error) {
	out := map[string]store.RepositorySummary{}
	get := func(repo string) store.RepositorySummary { return out[repo] }
	err := s.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT m.repository,
			       count(*) FILTER (WHERE m.kind = 'issue' AND NOT m.synced_closed),
			       count(*) FILTER (WHERE m.kind = 'issue' AND m.synced_closed),
			       count(*) FILTER (WHERE m.kind = 'pull' AND `+pairState+` = 'open'),
			       count(*) FILTER (WHERE m.kind = 'pull' AND `+pairState+` = 'merged'),
			       count(*) FILTER (WHERE m.kind = 'pull' AND `+pairState+` = 'closed')
			FROM issue_mapping m LEFT JOIN pull_request_state p ON p.issue_mapping_id = m.id
			GROUP BY m.repository`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var repo string
			var c store.PairCounts
			if err := rows.Scan(&repo, &c.IssuesOpen, &c.IssuesClosed, &c.PullsOpen, &c.PullsMerged, &c.PullsClosed); err != nil {
				rows.Close()
				return err
			}
			r := get(repo)
			r.Counts = c
			out[repo] = r
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			SELECT repository, count(*), count(*) FILTER (WHERE attempts > 0), min(marked_at)
			FROM dirty GROUP BY repository`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var repo string
			var q store.QueueStats
			var oldest *time.Time
			if err := rows.Scan(&repo, &q.Depth, &q.Failing, &oldest); err != nil {
				rows.Close()
				return err
			}
			if oldest != nil {
				q.Oldest = *oldest
			}
			r := get(repo)
			r.Queue = q
			out[repo] = r
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			SELECT repository, side, stream, cursor, updated_at FROM poll_cursor
			ORDER BY repository, side, stream`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var repo, side, stream string
			var c store.CursorReport
			if err := rows.Scan(&repo, &side, &stream, &c.Cursor, &c.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			c.Side, c.Stream = store.Side(side), store.Stream(stream)
			r := get(repo)
			r.Cursors = append(r.Cursors, c)
			out[repo] = r
		}
		return rows.Err()
	})
	return out, err
}

// pairState is a pair's state as the console names it. A pull request pair
// without a state yet was recovered from markers and counts by its issue.
const pairState = `(CASE
	WHEN p.state = 'merged' THEN 'merged'
	WHEN p.state = 'closed' THEN 'closed'
	WHEN p.state = 'open' THEN 'open'
	WHEN m.synced_closed THEN 'closed'
	ELSE 'open' END)`

// pairSelect reads a pair with its pull request state, comment count and
// queue entry. A pair's queue entry is keyed by its GitHub number; an entry
// from before the pair existed may be keyed by the Forgejo one.
const pairSelect = `
	SELECT m.id, m.repository, m.kind, m.github_number, m.forgejo_number, m.origin, m.synced_closed,
	       m.title, m.author, m.created_at, m.updated_at, ` + pairState + `,
	       (SELECT count(*) FROM comment_mapping c WHERE c.issue_mapping_id = m.id),
	       p.contrib_branch, p.imported_head_sha, p.state, p.head_frozen, p.touches_ci,
	       p.rebase_comment_id, p.rebase_requested, p.outcome_reported, p.updated_at,
	       d.side, d.number, d.marked_at, d.not_before, d.attempts, d.last_error
	FROM issue_mapping m
	LEFT JOIN pull_request_state p ON p.issue_mapping_id = m.id
	LEFT JOIN LATERAL (
		SELECT side, number, marked_at, not_before, attempts, last_error FROM dirty d
		WHERE d.repository = m.repository
		  AND ((d.side = 'github' AND d.number = m.github_number) OR (d.side = 'forgejo' AND d.number = m.forgejo_number))
		ORDER BY d.attempts DESC LIMIT 1
	) d ON true`

func scanPair(row pgx.Row) (store.PairReport, error) {
	var r store.PairReport
	var kind, origin, state string
	var branch, sha, pullState *string
	var frozen, touches, rebaseRequested, reported *bool
	var rebaseID *int64
	var pullUpdated *time.Time
	var dSide *string
	var dNumber *int64
	var dMarked, dNotBefore *time.Time
	var dAttempts *int
	var dError *string
	err := row.Scan(&r.ID, &r.Repository, &kind, &r.GitHubNumber, &r.ForgejoNumber, &origin, &r.SyncedClosed,
		&r.Title, &r.Author, &r.CreatedAt, &r.UpdatedAt, &state, &r.Comments,
		&branch, &sha, &pullState, &frozen, &touches, &rebaseID, &rebaseRequested, &reported, &pullUpdated,
		&dSide, &dNumber, &dMarked, &dNotBefore, &dAttempts, &dError)
	if err != nil {
		return r, err
	}
	r.Kind, r.Origin, r.State = store.Kind(kind), store.Side(origin), store.PairState(state)
	if pullState != nil {
		p := &store.PullReport{UpdatedAt: *pullUpdated}
		p.IssueMappingID = r.ID
		p.ContribBranch, p.ImportedHeadSHA, p.State = *branch, *sha, store.PullState(*pullState)
		p.HeadFrozen, p.TouchesCI, p.RebaseRequested, p.OutcomeReported = *frozen, *touches, *rebaseRequested, *reported
		if rebaseID != nil {
			p.RebaseCommentID = *rebaseID
		}
		r.Pull = p
	}
	if dSide != nil {
		q := &store.QueueReport{MarkedAt: *dMarked, NotBefore: *dNotBefore, Attempts: *dAttempts, PairID: r.ID, Kind: r.Kind}
		q.Repository, q.Side, q.Number = r.Repository, store.Side(*dSide), *dNumber
		if dError != nil {
			q.LastError = *dError
		}
		r.Queued = q
	}
	return r, nil
}

// pairColumns are what pairs sort by; the default is the newest first.
var pairColumns = map[string]string{
	"created":    "m.created_at",
	"updated":    "m.updated_at",
	"title":      "lower(m.title)",
	"repository": "m.repository",
	"author":     "lower(nullif(m.author, ''))",
	"github":     "m.github_number",
	"forgejo":    "m.forgejo_number",
	"state":      pairState,
	"comments":   "(SELECT count(*) FROM comment_mapping c WHERE c.issue_mapping_id = m.id)",
}

// Pairs implements store.Reports.
func (s *Store) Pairs(ctx context.Context, f store.PairFilter) ([]store.PairReport, int, *store.Position, error) {
	keys, err := sortKeys(f.Sort, "-created", pairColumns, []string{"m.id"})
	if err != nil {
		return nil, 0, nil, err
	}
	var where []string
	var args []any
	arg := placeholders(&args)
	if f.Repository != "" {
		where = append(where, "m.repository = "+arg(f.Repository))
	}
	if f.Kind != "" {
		where = append(where, "m.kind = "+arg(string(f.Kind)))
	}
	if f.State != "" {
		where = append(where, pairState+" = "+arg(string(f.State)))
	}
	if f.Origin != "" {
		where = append(where, "m.origin = "+arg(string(f.Origin)))
	}
	if f.Author != "" {
		where = append(where, "m.author = "+arg(f.Author))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		cond := "m.title ILIKE " + arg("%"+likeEscape(q)+"%")
		if n, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64); err == nil {
			p := arg(n)
			cond = "(" + cond + " OR m.github_number = " + p + " OR m.forgejo_number = " + p + ")"
		}
		where = append(where, cond)
	}
	const from = ` FROM issue_mapping m LEFT JOIN pull_request_state p ON p.issue_mapping_id = m.id`
	countQuery, countArgs := `SELECT count(*)`+from+clause(where), len(args)
	if f.After != nil {
		cond, err := keys.after(f.After, arg)
		if err != nil {
			return nil, 0, nil, err
		}
		where = append(where, cond)
	}
	limit := ""
	if f.Limit > 0 {
		limit = " LIMIT " + arg(f.Limit+1)
	}

	var out []store.PairReport
	var positions []store.Position
	var total int
	err = s.read(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, countQuery, args[:countArgs]...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT m.id, `+keys.selectKeys()+from+clause(where)+keys.orderBy()+limit, args...)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, values[0].(int64))
			positions = append(positions, position(values[1:]))
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// The page's pairs in full, in the page's order.
		full, err := tx.Query(ctx, pairSelect+` WHERE m.id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		byID := map[int64]store.PairReport{}
		for full.Next() {
			r, err := scanPair(full)
			if err != nil {
				full.Close()
				return err
			}
			byID[r.ID] = r
		}
		full.Close()
		for _, id := range ids {
			out = append(out, byID[id])
		}
		return full.Err()
	})
	if err != nil {
		return nil, 0, nil, fmt.Errorf("listing pairs: %w", err)
	}
	out, next := cut(out, positions, f.Limit)
	return out, total, next, nil
}

// cut trims a page read with one row too many, and says where the next page
// starts; nil when this was the last.
func cut[T any](rows []T, positions []store.Position, limit int) ([]T, *store.Position) {
	if limit <= 0 || len(rows) <= limit {
		return rows, nil
	}
	return rows[:limit], &positions[limit-1]
}

// clause joins conditions into a WHERE clause; empty for none.
func clause(where []string) string {
	if len(where) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(where, " AND ")
}

// likeEscape makes a search term match literally in an ILIKE pattern.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Pair implements store.Reports.
func (s *Store) Pair(ctx context.Context, id int64) (store.PairReport, error) {
	var r store.PairReport
	err := s.read(ctx, func(tx pgx.Tx) error {
		var err error
		r, err = scanPair(tx.QueryRow(ctx, pairSelect+` WHERE m.id = $1`, id))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return r, store.ErrNotFound
	}
	return r, err
}

// CommentPairs implements store.Reports.
func (s *Store) CommentPairs(ctx context.Context, id int64) ([]store.CommentReport, error) {
	var out []store.CommentReport
	err := s.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, issue_mapping_id, github_comment_id, forgejo_comment_id, origin, kind, created_at
			FROM comment_mapping WHERE issue_mapping_id = $1 ORDER BY id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c store.CommentReport
			var origin, kind string
			if err := rows.Scan(&c.ID, &c.IssueMappingID, &c.GitHubCommentID, &c.ForgejoCommentID, &origin, &kind, &c.CreatedAt); err != nil {
				return err
			}
			c.Origin, c.Kind = store.Side(origin), store.CommentKind(kind)
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// queueColumns are what the queue sorts by. The default, due, is failing
// entries first, then by when they are due: one text key, so that it sorts
// as one column.
var queueColumns = map[string]string{
	"due":        `((CASE WHEN d.attempts > 0 THEN '0' ELSE '1' END) || to_char(d.not_before AT TIME ZONE 'UTC', 'YYYYMMDDHH24MISSUS'))`,
	"marked":     "d.marked_at",
	"next":       "d.not_before",
	"attempts":   "d.attempts",
	"repository": "d.repository",
	"number":     "d.number",
	"error":      "d.last_error",
}

// Queue implements store.Reports.
func (s *Store) Queue(ctx context.Context, f store.QueueFilter) ([]store.QueueReport, int, *store.Position, error) {
	keys, err := sortKeys(f.Sort, "due", queueColumns, []string{"d.repository", "d.side", "d.number"})
	if err != nil {
		return nil, 0, nil, err
	}
	var where []string
	var args []any
	arg := placeholders(&args)
	if f.Repository != "" {
		where = append(where, "d.repository = "+arg(f.Repository))
	}
	if f.FailingOnly {
		where = append(where, "d.attempts > 0")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := arg("%" + likeEscape(q) + "%")
		cond := "(d.repository ILIKE " + like + " OR d.last_error ILIKE " + like
		if n, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64); err == nil {
			cond += " OR d.number = " + arg(n)
		}
		where = append(where, cond+")")
	}
	countQuery, countArgs := `SELECT count(*) FROM dirty d`+clause(where), len(args)
	if f.After != nil {
		cond, err := keys.after(f.After, arg)
		if err != nil {
			return nil, 0, nil, err
		}
		where = append(where, cond)
	}
	limit := ""
	if f.Limit > 0 {
		limit = " LIMIT " + arg(f.Limit+1)
	}

	var out []store.QueueReport
	var positions []store.Position
	var total int
	err = s.read(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, countQuery, args[:countArgs]...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT d.repository, d.side, d.number, d.marked_at, d.not_before, d.attempts, d.last_error, m.id, m.kind,
			       `+keys.selectKeys()+`
			FROM dirty d
			LEFT JOIN LATERAL (
				SELECT id, kind FROM issue_mapping m
				WHERE m.repository = d.repository
				  AND ((d.side = 'github' AND m.github_number = d.number) OR (d.side = 'forgejo' AND m.forgejo_number = d.number))
				LIMIT 1
			) m ON true`+clause(where)+keys.orderBy()+limit, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			q := store.QueueReport{Attempts: int(values[5].(int32))}
			q.Repository, q.Side, q.Number = values[0].(string), store.Side(values[1].(string)), values[2].(int64)
			q.MarkedAt, q.NotBefore = values[3].(time.Time), values[4].(time.Time)
			if e, ok := values[6].(string); ok {
				q.LastError = e
			}
			if id, ok := values[7].(int64); ok {
				q.PairID, q.Kind = id, store.Kind(values[8].(string))
			}
			out = append(out, q)
			positions = append(positions, position(values[9:]))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, nil, fmt.Errorf("listing the queue: %w", err)
	}
	out, next := cut(out, positions, f.Limit)
	return out, total, next, nil
}

// RateLimits implements store.Reports.
func (s *Store) RateLimits(ctx context.Context) ([]store.RateLimit, error) {
	var out []store.RateLimit
	err := s.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT resource, "limit", remaining, used, reset_at, observed_at FROM github_rate_limit ORDER BY resource`)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.RateLimit, error) {
			var l store.RateLimit
			return l, row.Scan(&l.Resource, &l.Limit, &l.Remaining, &l.Used, &l.ResetAt, &l.ObservedAt)
		})
		return err
	})
	return out, err
}

// authorColumns are what authors sort by; the default is the most active
// first.
var authorColumns = map[string]string{
	"pairs":  "a.pairs",
	"issues": "a.issues",
	"pulls":  "a.pulls",
	"last":   "a.last",
	"login":  "lower(a.author)",
}

// Authors implements store.Reports.
func (s *Store) Authors(ctx context.Context, f store.AuthorFilter) ([]store.Author, int, *store.Position, error) {
	keys, err := sortKeys(f.Sort, "-pairs", authorColumns, []string{"a.author", "a.origin"})
	if err != nil {
		return nil, 0, nil, err
	}
	var args []any
	arg := placeholders(&args)
	search := ""
	if q := strings.TrimSpace(f.Query); q != "" {
		search += " AND author ILIKE " + arg("%"+likeEscape(q)+"%")
	}
	if f.Side != "" {
		search += " AND origin = " + arg(string(f.Side))
	}
	var where []string
	switch f.Kind {
	case store.KindIssue:
		where = append(where, "a.issues > 0")
	case store.KindPull:
		where = append(where, "a.pulls > 0")
	}
	authors := `(SELECT author, origin, count(*) FILTER (WHERE kind = 'issue') AS issues,
		       count(*) FILTER (WHERE kind = 'pull') AS pulls, count(*) AS pairs, max(created_at) AS last
		FROM issue_mapping WHERE author <> ''` + search + `
		GROUP BY author, origin) a`
	countArgs := len(args)
	countWhere := clause(where)
	if f.After != nil {
		cond, err := keys.after(f.After, arg)
		if err != nil {
			return nil, 0, nil, err
		}
		where = append(where, cond)
	}
	limit := ""
	if f.Limit > 0 {
		limit = " LIMIT " + arg(f.Limit+1)
	}

	var out []store.Author
	var positions []store.Position
	var total int
	err = s.read(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM `+authors+countWhere, args[:countArgs]...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT a.author, a.origin, a.issues, a.pulls, a.last, `+keys.selectKeys()+
			` FROM `+authors+clause(where)+keys.orderBy()+limit, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			out = append(out, store.Author{Login: values[0].(string), Side: store.Side(values[1].(string)),
				Issues: int(values[2].(int64)), Pulls: int(values[3].(int64)), Last: values[4].(time.Time)})
			positions = append(positions, position(values[5:]))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, nil, fmt.Errorf("listing authors: %w", err)
	}
	out, next := cut(out, positions, f.Limit)
	return out, total, next, nil
}
