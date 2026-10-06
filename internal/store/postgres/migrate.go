package postgres

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID is the advisory lock every migrating process contends for.
//
// Two replicas start at once on a rollout, and without this they both see an
// unapplied migration and both run it. The value is arbitrary but must never
// change: it identifies this service's migrations to PostgreSQL, and two
// services sharing a database and a number would serialise against each other
// for no reason.
const migrationLockID int64 = 0x464742524d494752 // "FGBRMIGR"

// Migrate applies every migration the binary carries that the database has not
// seen, in file-name order.
//
// This is deliberately not a migration framework. What a framework buys is
// down-migrations and out-of-order application; what it costs is a dependency
// and a second tool in the release path. Neither purchase pays here: the schema
// is small, and a rollback of it is a restore from a backup, not
// a down-migration.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	files, err := migrationNames()
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring a connection for migration: %w", err)
	}
	defer conn.Release()

	// The lock is session-scoped and taken outside the per-migration
	// transactions, so it still covers the gap between two of them.
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("taking the migration lock: %w", err)
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID); err != nil {
			log.Warn("releasing the migration lock failed; it is released when the connection closes", "error", err)
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migration (
			name        text        PRIMARY KEY,
			checksum    char(64)    NOT NULL,
			applied_at  timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("creating the migration table: %w", err)
	}

	applied, err := appliedMigrations(ctx, conn.Conn())
	if err != nil {
		return err
	}

	for _, name := range files {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		checksum := hex.EncodeToString(sum[:])

		if have, ok := applied[name]; ok {
			// An applied migration whose content changed means the schema in the
			// database is not the one this binary believes it wrote. Continuing
			// would run every later migration against an unknown base.
			if have != checksum {
				return fmt.Errorf("migration %s was applied with a different content (recorded %s, have %s): "+
					"an applied migration must never be edited", name, have[:12], checksum[:12])
			}
			continue
		}

		log.Info("applying migration", "name", name)
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migration (name, checksum) VALUES ($1, $2)`, name, checksum); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("recording migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing migration %s: %w", name, err)
		}
	}
	return nil
}

// CheckSchema verifies that every migration this binary carries has been
// applied, for a process that does not migrate.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	files, err := migrationNames()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring a connection: %w", err)
	}
	defer conn.Release()
	applied, err := appliedMigrations(ctx, conn.Conn())
	if err != nil {
		return fmt.Errorf("%w; has a bridge ever started on this database?", err)
	}
	for _, name := range files {
		if _, ok := applied[name]; !ok {
			return fmt.Errorf("the database lacks migration %s: start a bridge of this version first, which applies it", name)
		}
	}
	return nil
}

// migrationNames lists the embedded migrations in the order they apply.
func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	// File-name order, which is why they are numbered. Ordering by anything
	// derived from the filesystem would make the schema depend on the build host.
	sort.Strings(names)
	return names, nil
}

func appliedMigrations(ctx context.Context, conn *pgx.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, `SELECT name, checksum FROM schema_migration`)
	if err != nil {
		return nil, fmt.Errorf("reading applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]string{}
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, fmt.Errorf("scanning applied migrations: %w", err)
		}
		applied[name] = checksum
	}
	return applied, rows.Err()
}
