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

// queueLimit bounds a queue listing. A queue this long is a problem the
// dashboard shows; the console need not list every entry of it.
const queueLimit = 500

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
		q := &store.QueueReport{MarkedAt: *dMarked, NotBefore: *dNotBefore, Attempts: *dAttempts, PairID: r.ID}
		q.Repository, q.Side, q.Number = r.Repository, store.Side(*dSide), *dNumber
		if dError != nil {
			q.LastError = *dError
		}
		r.Queued = q
	}
	return r, nil
}

// Pairs implements store.Reports.
func (s *Store) Pairs(ctx context.Context, f store.PairFilter) ([]store.PairReport, int, error) {
	var where []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
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
	if q := strings.TrimSpace(f.Query); q != "" {
		cond := "m.title ILIKE " + arg("%"+likeEscape(q)+"%")
		if n, err := strconv.ParseInt(strings.TrimPrefix(q, "#"), 10, 64); err == nil {
			p := arg(n)
			cond = "(" + cond + " OR m.github_number = " + p + " OR m.forgejo_number = " + p + ")"
		}
		where = append(where, cond)
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}

	var out []store.PairReport
	var total int
	err := s.read(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM issue_mapping m
			LEFT JOIN pull_request_state p ON p.issue_mapping_id = m.id`+clause, args...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, pairSelect+clause+
			" ORDER BY m.created_at DESC, m.id DESC LIMIT "+arg(limit)+" OFFSET "+arg(offset), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanPair(rows)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, fmt.Errorf("listing pairs: %w", err)
	}
	return out, total, nil
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
			SELECT id, issue_mapping_id, github_comment_id, forgejo_comment_id, origin, created_at
			FROM comment_mapping WHERE issue_mapping_id = $1 ORDER BY id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c store.CommentReport
			var origin string
			if err := rows.Scan(&c.ID, &c.IssueMappingID, &c.GitHubCommentID, &c.ForgejoCommentID, &origin, &c.CreatedAt); err != nil {
				return err
			}
			c.Origin = store.Side(origin)
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// Queue implements store.Reports.
func (s *Store) Queue(ctx context.Context, f store.QueueFilter) ([]store.QueueReport, error) {
	var where []string
	var args []any
	if f.Repository != "" {
		args = append(args, f.Repository)
		where = append(where, "d.repository = $1")
	}
	if f.FailingOnly {
		where = append(where, "d.attempts > 0")
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var out []store.QueueReport
	err := s.read(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT d.repository, d.side, d.number, d.marked_at, d.not_before, d.attempts, d.last_error, m.id
			FROM dirty d
			LEFT JOIN LATERAL (
				SELECT id FROM issue_mapping m
				WHERE m.repository = d.repository
				  AND ((d.side = 'github' AND m.github_number = d.number) OR (d.side = 'forgejo' AND m.forgejo_number = d.number))
				LIMIT 1
			) m ON true`+clause+`
			ORDER BY (d.attempts > 0) DESC, d.not_before, d.repository, d.side, d.number
			LIMIT `+strconv.Itoa(queueLimit), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var q store.QueueReport
			var side string
			var lastError *string
			var pair *int64
			if err := rows.Scan(&q.Repository, &side, &q.Number, &q.MarkedAt, &q.NotBefore, &q.Attempts, &lastError, &pair); err != nil {
				return err
			}
			q.Side = store.Side(side)
			if lastError != nil {
				q.LastError = *lastError
			}
			if pair != nil {
				q.PairID = *pair
			}
			out = append(out, q)
		}
		return rows.Err()
	})
	return out, err
}
