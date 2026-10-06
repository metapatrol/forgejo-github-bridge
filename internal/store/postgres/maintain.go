package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// maintenanceLockID keeps two rounds from running at once, on any instance.
// Like the other lock IDs it must never change.
const maintenanceLockID int64 = 0x464742524d41494e // "FGBRMAIN"

// keptRounds is how many maintenance rounds the database remembers.
const keptRounds = 20

// Maintain runs a database maintenance round whenever one is due, until ctx
// ends: one interval after the last round finished, on any instance, and a
// minute after it starts when there was none. A round vacuums and analyzes
// every table, drops what interrupted index rebuilds left behind and
// rebuilds invalid indexes. Autovacuum keeps the bridge's small tables well
// on its own; the round is the backstop, and the console shows it
// (docs/console.md). The pattern is engram's.
//
// The active instance runs it; interval zero turns it off.
func Maintain(ctx context.Context, pool *pgxpool.Pool, interval time.Duration, log *slog.Logger) {
	if interval <= 0 {
		log.Info("database maintenance is off")
		return
	}
	wait := time.Minute
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = time.Minute
		last, err := lastRound(ctx, pool)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn("cannot tell when the last database maintenance round was", "error", err)
			}
			continue
		}
		if due := last.Add(interval); !last.IsZero() && time.Now().Before(due) {
			wait = max(time.Until(due), time.Minute)
			continue
		}
		if err := RunMaintenance(ctx, pool, log); err != nil && ctx.Err() == nil {
			log.Error("database maintenance round failed", "error", err)
		}
	}
}

func lastRound(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	var last *time.Time
	err := pool.QueryRow(ctx, `SELECT max(finished_at) FROM maintenance_round`).Scan(&last)
	if err != nil || last == nil {
		return time.Time{}, err
	}
	return *last, nil
}

// RunMaintenance runs one round now, unless one is running already.
func RunMaintenance(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, maintenanceLockID).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, maintenanceLockID)
	}()

	var id int64
	if err := conn.QueryRow(ctx, `INSERT INTO maintenance_round DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		return fmt.Errorf("recording the round: %w", err)
	}
	start := time.Now()
	var tables, rebuilt, dropped, failures []string
	fail := func(what string, err error) { failures = append(failures, fmt.Sprintf("%s: %v", what, err)) }

	var schema string
	if err := conn.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		fail("schema", err)
	} else {
		// What a REINDEX CONCURRENTLY that was interrupted leaves behind.
		leftovers, err := names(ctx, conn, `
			SELECT indexrelname FROM pg_stat_user_indexes
			WHERE schemaname = current_schema() AND (indexrelname LIKE '%\_ccnew%' OR indexrelname LIKE '%\_ccold%')
			ORDER BY indexrelname`)
		if err != nil {
			fail("finding leftover indexes", err)
		}
		for _, ix := range leftovers {
			if _, err := conn.Exec(ctx, `DROP INDEX CONCURRENTLY IF EXISTS `+pgx.Identifier{schema, ix}.Sanitize()); err != nil {
				fail("drop "+ix, err)
				continue
			}
			dropped = append(dropped, ix)
		}
		invalid, err := names(ctx, conn, `
			SELECT s.indexrelname FROM pg_stat_user_indexes s JOIN pg_index i ON i.indexrelid = s.indexrelid
			WHERE s.schemaname = current_schema() AND NOT (i.indisvalid AND i.indisready)
			ORDER BY s.indexrelname`)
		if err != nil {
			fail("finding invalid indexes", err)
		}
		for _, ix := range invalid {
			if strings.Contains(ix, "_ccnew") || strings.Contains(ix, "_ccold") {
				continue
			}
			if _, err := conn.Exec(ctx, `REINDEX INDEX CONCURRENTLY `+pgx.Identifier{schema, ix}.Sanitize()); err != nil {
				fail("rebuild "+ix, err)
				continue
			}
			rebuilt = append(rebuilt, ix)
		}
		all, err := names(ctx, conn, `
			SELECT relname FROM pg_stat_user_tables WHERE schemaname = current_schema() ORDER BY relname`)
		if err != nil {
			fail("finding tables", err)
		}
		for _, t := range all {
			if _, err := conn.Exec(ctx, `VACUUM (ANALYZE) `+pgx.Identifier{schema, t}.Sanitize()); err != nil {
				fail("vacuum "+t, err)
				continue
			}
			tables = append(tables, t)
		}
	}

	if _, err := conn.Exec(ctx, `
		UPDATE maintenance_round SET finished_at = now(), tables = $2, rebuilt = $3, dropped = $4, failures = $5
		WHERE id = $1`, id, nonNil(tables), nonNil(rebuilt), nonNil(dropped), nonNil(failures)); err != nil {
		return fmt.Errorf("recording the round's end: %w", err)
	}
	if _, err := conn.Exec(ctx, `
		DELETE FROM maintenance_round WHERE id NOT IN (SELECT id FROM maintenance_round ORDER BY id DESC LIMIT $1)`,
		keptRounds); err != nil {
		log.Warn("forgetting old maintenance rounds failed", "error", err)
	}
	level := slog.LevelInfo
	if len(failures) > 0 {
		level = slog.LevelWarn
	}
	log.Log(ctx, level, "database maintenance round done", "tables", len(tables), "rebuilt", rebuilt,
		"dropped", dropped, "failures", failures, "duration", time.Since(start).Round(time.Millisecond))
	return nil
}

func names(ctx context.Context, conn *pgxpool.Conn, query string) ([]string, error) {
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
