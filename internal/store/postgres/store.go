// Package postgres is the PostgreSQL store, run on CloudNativePG.
//
// It is the only package that knows the schema. Everything above it works in
// the types of package store.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// Store owns the connection pool.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

var _ store.Store = (*Store)(nil)

// Open connects, verifies the connection and applies pending migrations.
//
// Migrating at startup rather than from a separate job: the bridge owns its
// schema entirely, and a job is a second thing that can be forgotten in a
// rollout. The advisory lock in Migrate makes it safe with two replicas.
func Open(ctx context.Context, dsn string, maxConns int32, log *slog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// The DSN carries a password. Reporting the parse error alone keeps it
		// out of the log line that a failed startup writes.
		return nil, fmt.Errorf("parsing the database DSN: %w", err)
	}
	cfg.MaxConns = maxConns

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating the connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to the database: %w", err)
	}

	if err := Migrate(ctx, pool, log); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, log: log}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping reports whether the database answers. It backs the readiness probe.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Pool exposes the pool to the leader lock.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// conflict maps a unique violation to store.ErrConflict.
func conflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23503") {
		return fmt.Errorf("%w: %s", store.ErrConflict, pgErr.ConstraintName)
	}
	return err
}

const issueColumns = `id, repository, github_number, forgejo_number, origin, synced_closed`

func scanIssue(row pgx.Row) (store.IssueMapping, error) {
	var m store.IssueMapping
	var origin string
	err := row.Scan(&m.ID, &m.Repository, &m.GitHubNumber, &m.ForgejoNumber, &origin, &m.SyncedClosed)
	m.Origin = store.Side(origin)
	return m, err
}

func (s *Store) issueWhere(ctx context.Context, where string, args ...any) (store.IssueMapping, bool, error) {
	m, err := scanIssue(s.pool.QueryRow(ctx, `SELECT `+issueColumns+` FROM issue_mapping WHERE `+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.IssueMapping{}, false, nil
	}
	if err != nil {
		return store.IssueMapping{}, false, fmt.Errorf("reading an issue mapping: %w", err)
	}
	return m, true, nil
}

// IssueByGitHub implements store.Store.
func (s *Store) IssueByGitHub(ctx context.Context, repo string, n int64) (store.IssueMapping, bool, error) {
	return s.issueWhere(ctx, `repository = $1 AND github_number = $2`, repo, n)
}

// IssueByForgejo implements store.Store.
func (s *Store) IssueByForgejo(ctx context.Context, repo string, n int64) (store.IssueMapping, bool, error) {
	return s.issueWhere(ctx, `repository = $1 AND forgejo_number = $2`, repo, n)
}

// InsertIssue implements store.Store.
func (s *Store) InsertIssue(ctx context.Context, m store.IssueMapping) (store.IssueMapping, error) {
	out, err := scanIssue(s.pool.QueryRow(ctx, `
		INSERT INTO issue_mapping (repository, github_number, forgejo_number, origin, synced_closed)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+issueColumns,
		m.Repository, m.GitHubNumber, m.ForgejoNumber, string(m.Origin), m.SyncedClosed))
	if err != nil {
		return store.IssueMapping{}, conflict(err)
	}
	return out, nil
}

// SetIssueSyncedClosed implements store.Store.
func (s *Store) SetIssueSyncedClosed(ctx context.Context, id int64, closed bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE issue_mapping SET synced_closed = $2, updated_at = now() WHERE id = $1`, id, closed)
	return err
}

// DeleteIssue implements store.Store.
func (s *Store) DeleteIssue(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM issue_mapping WHERE id = $1`, id)
	return err
}

// OpenIssues implements store.Store.
func (s *Store) OpenIssues(ctx context.Context, repo string) ([]store.IssueMapping, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+issueColumns+` FROM issue_mapping WHERE repository = $1 AND NOT synced_closed ORDER BY id`, repo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.IssueMapping
	for rows.Next() {
		m, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const commentColumns = `id, issue_mapping_id, github_comment_id, forgejo_comment_id, origin`

func scanComment(row pgx.Row) (store.CommentMapping, error) {
	var c store.CommentMapping
	var origin string
	err := row.Scan(&c.ID, &c.IssueMappingID, &c.GitHubCommentID, &c.ForgejoCommentID, &origin)
	c.Origin = store.Side(origin)
	return c, err
}

// Comments implements store.Store.
func (s *Store) Comments(ctx context.Context, issueID int64) ([]store.CommentMapping, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+commentColumns+` FROM comment_mapping WHERE issue_mapping_id = $1 ORDER BY id`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.CommentMapping
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertComment implements store.Store.
func (s *Store) InsertComment(ctx context.Context, c store.CommentMapping) (store.CommentMapping, error) {
	out, err := scanComment(s.pool.QueryRow(ctx, `
		INSERT INTO comment_mapping (issue_mapping_id, github_comment_id, forgejo_comment_id, origin)
		VALUES ($1, $2, $3, $4)
		RETURNING `+commentColumns,
		c.IssueMappingID, c.GitHubCommentID, c.ForgejoCommentID, string(c.Origin)))
	if err != nil {
		return store.CommentMapping{}, conflict(err)
	}
	return out, nil
}

// UpdateComment implements store.Store.
func (s *Store) UpdateComment(ctx context.Context, c store.CommentMapping) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE comment_mapping
		SET github_comment_id = $2, forgejo_comment_id = $3, updated_at = now()
		WHERE id = $1`, c.ID, c.GitHubCommentID, c.ForgejoCommentID)
	return conflict(err)
}

// DeleteComment implements store.Store.
func (s *Store) DeleteComment(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM comment_mapping WHERE id = $1`, id)
	return err
}

// Cursor implements store.Store.
func (s *Store) Cursor(ctx context.Context, repo string, side store.Side, stream store.Stream) (time.Time, bool, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT cursor FROM poll_cursor WHERE repository = $1 AND side = $2 AND stream = $3`,
		repo, string(side), string(stream)).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return at, err == nil, err
}

// SetCursor implements store.Store.
func (s *Store) SetCursor(ctx context.Context, repo string, side store.Side, stream store.Stream, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO poll_cursor (repository, side, stream, cursor) VALUES ($1, $2, $3, $4)
		ON CONFLICT (repository, side, stream) DO UPDATE SET cursor = excluded.cursor, updated_at = now()`,
		repo, string(side), string(stream), at)
	return err
}

// MarkDirty implements store.Store.
func (s *Store) MarkDirty(ctx context.Context, keys ...store.DirtyKey) error {
	if len(keys) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, k := range keys {
		// Only marked_at moves on a conflict. not_before and attempts are the
		// entry's backoff, and a burst of changes to a resource that keeps
		// failing must not turn into a burst of failing attempts.
		batch.Queue(`
			INSERT INTO dirty (repository, side, number) VALUES ($1, $2, $3)
			ON CONFLICT (repository, side, number) DO UPDATE SET marked_at = clock_timestamp()`,
			k.Repository, string(k.Side), k.Number)
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// NextDirty implements store.Store.
func (s *Store) NextDirty(ctx context.Context) (store.Dirty, bool, error) {
	var d store.Dirty
	var side string
	err := s.pool.QueryRow(ctx, `
		SELECT repository, side, number, marked_at, attempts FROM dirty
		WHERE not_before <= clock_timestamp()
		ORDER BY not_before, repository, side, number
		LIMIT 1`).Scan(&d.Repository, &side, &d.Number, &d.MarkedAt, &d.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Dirty{}, false, nil
	}
	if err != nil {
		return store.Dirty{}, false, err
	}
	d.Side = store.Side(side)
	return d, true, nil
}

// ClearDirty implements store.Store.
func (s *Store) ClearDirty(ctx context.Context, d store.Dirty) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM dirty WHERE repository = $1 AND side = $2 AND number = $3 AND marked_at = $4`,
		d.Repository, string(d.Side), d.Number, d.MarkedAt)
	return tag.RowsAffected() == 1, err
}

// DeferDirty implements store.Store.
func (s *Store) DeferDirty(ctx context.Context, d store.Dirty, until time.Time, reason string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE dirty SET attempts = attempts + 1, not_before = $4, last_error = $5
		WHERE repository = $1 AND side = $2 AND number = $3`,
		d.Repository, string(d.Side), d.Number, until, reason)
	return err
}

// DirtyStats implements store.Store.
func (s *Store) DirtyStats(ctx context.Context) (int, time.Time, error) {
	var n int
	var oldest *time.Time
	err := s.pool.QueryRow(ctx, `SELECT count(*), min(marked_at) FROM dirty`).Scan(&n, &oldest)
	if err != nil || oldest == nil {
		return n, time.Time{}, err
	}
	return n, *oldest, nil
}
