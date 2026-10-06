package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// systemSettings are the server settings the system page shows: the ones
// that decide how the bridge's small, busy tables are kept.
var systemSettings = []string{
	"shared_buffers", "work_mem", "maintenance_work_mem", "autovacuum",
	"autovacuum_naptime", "autovacuum_vacuum_scale_factor", "autovacuum_analyze_scale_factor",
	"statement_timeout", "idle_in_transaction_session_timeout",
}

// System implements store.Reports. The queries are engram's, which keeps the
// metapatrol consoles' system pages comparable.
func (s *Store) System(ctx context.Context) (store.System, error) {
	var out store.System
	err := s.read(ctx, func(tx pgx.Tx) error {
		d := &out.Database
		var reset *time.Time
		if err := tx.QueryRow(ctx, `
			SELECT current_setting('server_version'), pg_database_size(current_database()),
			       d.numbackends, current_setting('max_connections')::bigint,
			       d.blks_hit, d.blks_read, d.deadlocks, d.temp_bytes, d.stats_reset,
			       (SELECT age(datfrozenxid) FROM pg_database WHERE datname = current_database())::bigint,
			       current_setting('autovacuum_freeze_max_age')::bigint
			FROM pg_stat_database d WHERE d.datname = current_database()`).Scan(
			&d.Version, &d.SizeBytes, &d.Connections, &d.MaxConnections,
			&d.BlocksHit, &d.BlocksRead, &d.Deadlocks, &d.TempBytes, &reset, &d.XIDAge, &d.FreezeMaxAge); err != nil {
			return fmt.Errorf("reading the database: %w", err)
		}
		if reset != nil {
			d.StatsReset = *reset
		}
		stat := s.pool.Stat()
		d.PoolSize, d.PoolInUse, d.PoolMax = int64(stat.TotalConns()), int64(stat.AcquiredConns()), int64(stat.MaxConns())

		rows, err := tx.Query(ctx, `
			SELECT s.name, coalesce(current_setting(s.name, true), '')
			FROM unnest($1::text[]) WITH ORDINALITY AS s (name, i) ORDER BY s.i`, systemSettings)
		if err != nil {
			return err
		}
		if d.Settings, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.Setting, error) {
			var st store.Setting
			return st, row.Scan(&st.Name, &st.Value)
		}); err != nil {
			return fmt.Errorf("reading the settings: %w", err)
		}

		if out.Tables, err = systemTables(ctx, tx); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `SELECT name, applied_at FROM schema_migration ORDER BY name`)
		if err != nil {
			return err
		}
		if out.Migrations, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.Migration, error) {
			var m store.Migration
			return m, row.Scan(&m.Name, &m.AppliedAt)
		}); err != nil {
			return fmt.Errorf("reading the migrations: %w", err)
		}

		rows, err = tx.Query(ctx, `
			SELECT left(regexp_replace(query, '\s+', ' ', 'g'), 120) FROM pg_stat_activity
			WHERE datname = current_database() AND pid <> pg_backend_pid() AND state <> 'idle'
			  AND query ~* '^\s*(reindex|vacuum|analyze|autovacuum)'
			ORDER BY query_start`)
		if err != nil {
			return err
		}
		if out.Running, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return fmt.Errorf("reading what maintenance runs: %w", err)
		}

		rows, err = tx.Query(ctx, `
			SELECT started_at, finished_at, tables, rebuilt, dropped, failures
			FROM maintenance_round ORDER BY id DESC`)
		if err != nil {
			return err
		}
		if out.Rounds, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.MaintenanceRound, error) {
			var m store.MaintenanceRound
			var finished *time.Time
			err := row.Scan(&m.StartedAt, &finished, &m.Tables, &m.Rebuilt, &m.Dropped, &m.Failures)
			if finished != nil {
				m.FinishedAt = *finished
			}
			return m, err
		}); err != nil {
			return fmt.Errorf("reading the maintenance rounds: %w", err)
		}
		return nil
	})
	return out, err
}

func systemTables(ctx context.Context, tx pgx.Tx) ([]store.SystemTable, error) {
	// A table's autovacuum thresholds: its own storage options where it has
	// them, the server's settings otherwise, scaled by its estimated rows.
	rows, err := tx.Query(ctx, `
		WITH t AS (
		       SELECT s.*, greatest(c.reltuples, 0) AS tuples,
		              (SELECT array_agg(option_name || '=' || option_value) FROM pg_options_to_table(c.reloptions)) AS options
		         FROM pg_stat_user_tables s JOIN pg_class c ON c.oid = s.relid
		        WHERE s.schemaname = current_schema()
		     ), o AS (
		       SELECT t.*,
		              (SELECT split_part(x, '=', 2) FROM unnest(options) x WHERE x LIKE 'autovacuum\_vacuum\_threshold=%') AS vt,
		              (SELECT split_part(x, '=', 2) FROM unnest(options) x WHERE x LIKE 'autovacuum\_vacuum\_scale\_factor=%') AS vs,
		              (SELECT split_part(x, '=', 2) FROM unnest(options) x WHERE x LIKE 'autovacuum\_analyze\_threshold=%') AS ath,
		              (SELECT split_part(x, '=', 2) FROM unnest(options) x WHERE x LIKE 'autovacuum\_analyze\_scale\_factor=%') AS ascale
		         FROM t
		     )
		SELECT relname, pg_total_relation_size(relid), pg_relation_size(relid), pg_indexes_size(relid),
		       n_live_tup, n_dead_tup, n_mod_since_analyze,
		       (coalesce(vt, current_setting('autovacuum_vacuum_threshold'))::float8
		        + coalesce(vs, current_setting('autovacuum_vacuum_scale_factor'))::float8 * tuples)::bigint,
		       (coalesce(ath, current_setting('autovacuum_analyze_threshold'))::float8
		        + coalesce(ascale, current_setting('autovacuum_analyze_scale_factor'))::float8 * tuples)::bigint,
		       last_vacuum, last_autovacuum, last_analyze, last_autoanalyze
		  FROM o
		 ORDER BY pg_total_relation_size(relid) DESC, relname`)
	if err != nil {
		return nil, err
	}
	tables, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.SystemTable, error) {
		var t store.SystemTable
		var lv, lav, la, laa *time.Time
		err := row.Scan(&t.Name, &t.TotalBytes, &t.HeapBytes, &t.IndexBytes,
			&t.LiveRows, &t.DeadRows, &t.ModifiedSinceAnalyze, &t.VacuumThreshold, &t.AnalyzeThreshold,
			&lv, &lav, &la, &laa)
		for _, p := range []struct {
			src *time.Time
			dst *time.Time
		}{{lv, &t.LastVacuum}, {lav, &t.LastAutovacuum}, {la, &t.LastAnalyze}, {laa, &t.LastAutoanalyze}} {
			if p.src != nil {
				*p.dst = *p.src
			}
		}
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("reading the tables: %w", err)
	}

	rows, err = tx.Query(ctx, `
		SELECT s.relname, s.indexrelname, a.amname, pg_relation_size(s.indexrelid), s.idx_scan,
		       i.indisvalid AND i.indisready
		  FROM pg_stat_user_indexes s
		  JOIN pg_index i ON i.indexrelid = s.indexrelid
		  JOIN pg_class c ON c.oid = s.indexrelid
		  JOIN pg_am a ON a.oid = c.relam
		 WHERE s.schemaname = current_schema()
		 ORDER BY pg_relation_size(s.indexrelid) DESC, s.indexrelname`)
	if err != nil {
		return nil, err
	}
	byTable := map[string][]store.SystemIndex{}
	var table string
	var ix store.SystemIndex
	if _, err := pgx.ForEachRow(rows, []any{&table, &ix.Name, &ix.Method, &ix.Bytes, &ix.Scans, &ix.Valid}, func() error {
		byTable[table] = append(byTable[table], ix)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("reading the indexes: %w", err)
	}
	for i := range tables {
		tables[i].Indexes = byTable[tables[i].Name]
	}
	return tables, nil
}
