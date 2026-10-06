import { ChangeDetectionStrategy, Component, computed, DestroyRef, inject } from '@angular/core';
import {
  ago,
  bytes,
  count,
  MpClock,
  MpIcon,
  MpLoadState,
  MpPageHeader,
  MpTime,
  MpTip,
  span,
  timestamp,
} from '@metapatrol/ng';
import { getSystem } from '../../api/fn/status/get-system';
import { SystemDatabase } from '../../api/models/system-database';
import { MaintenanceRound } from '../../api/models/maintenance-round';
import { Migration } from '../../api/models/migration';
import { SystemTable } from '../../api/models/system-table';
import { apiResource } from '../../core/api';
import { percentLeft, rateTone } from '../../core/rate-limit';
import { StatusService } from '../../core/status';
import { until } from '../../core/format';
import { liveReload } from '../../core/events';
import { Meter } from '../../shared/meter';

/** How often the page reloads on its own: the database has no events, and its figures move slowly. */
export const SYSTEM_REFRESH_MS = 30_000;

/** The later of a maintenance round (or another manual run) and autovacuum, and which it was. */
export function lastOf(
  manual: string | undefined,
  auto: string | undefined,
): { at: string; by: 'round' | 'autovacuum' } | undefined {
  if (!manual && !auto) {
    return undefined;
  }
  if (!auto || (manual && manual > auto)) {
    return { at: manual!, by: 'round' };
  }
  return { at: auto, by: 'autovacuum' };
}

/** Of all blocks read, the share found in shared buffers; 1 before anything was read. */
export function cacheHitRatio(d: Pick<SystemDatabase, 'blocksHit' | 'blocksRead'>): number {
  const total = d.blocksHit + d.blocksRead;
  return total > 0 ? d.blocksHit / total : 1;
}

/** What a maintenance round did, in a line: "8 tables vacuumed · rebuilt x · dropped y". */
export function roundSummary(r: MaintenanceRound): string {
  const parts = [`${r.tables.length} ${r.tables.length === 1 ? 'table' : 'tables'} vacuumed`];
  if (r.rebuilt.length) {
    parts.push(`rebuilt ${r.rebuilt.join(', ')}`);
  }
  if (r.dropped.length) {
    parts.push(`dropped ${r.dropped.join(', ')}`);
  }
  return parts.join(' · ');
}

/** How long a finished round took; undefined while it runs. */
export function roundDuration(r: MaintenanceRound): number | undefined {
  return r.finishedAt ? Date.parse(r.finishedAt) - Date.parse(r.startedAt) : undefined;
}

/**
 * The schema as one line: the newest migration without .sql, how many there
 * are, and every one with when it was applied, for a tooltip.
 */
export function schemaLine(
  migrations: readonly Migration[],
): { newest: string; count: number; tip: string } | undefined {
  if (!migrations.length) {
    return undefined;
  }
  const sorted = [...migrations].sort((a, b) => a.name.localeCompare(b.name));
  return {
    newest: sorted[sorted.length - 1].name.replace(/\.sql$/, ''),
    count: sorted.length,
    tip: sorted.map((m) => `${m.name}  ${timestamp(m.appliedAt)}`).join('\n'),
  };
}

/** 60 → "1 min", 21600 → "6 h", 45 → "45 s". */
export function interval(seconds: number): string {
  if (seconds % 3600 === 0) {
    return `${seconds / 3600} h`;
  }
  if (seconds % 60 === 0) {
    return `${seconds / 60} min`;
  }
  return `${seconds} s`;
}

@Component({
  selector: 'app-system',
  imports: [MpIcon, MpLoadState, MpPageHeader, MpTime, MpTip, Meter],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './system.html',
})
export class System {
  private readonly clock = inject(MpClock);
  protected readonly bytes = bytes;
  protected readonly count = count;
  protected readonly ago = ago;
  protected readonly timestamp = timestamp;
  protected readonly interval = interval;
  protected readonly lastOf = lastOf;
  protected readonly percentLeft = percentLeft;
  protected readonly roundSummary = roundSummary;
  protected readonly coreNote = 'core is the REST API the bridge uses.';
  protected readonly rateNote =
    "The GitHub App installation's rate-limit windows. core is the REST API the bridge uses; the figures are GitHub's, from its last response.";
  protected readonly schema = computed(() => schemaLine(this.res.body()?.migrations ?? []));
  /** The newest round, if any; it runs while it has no end. */
  protected readonly lastRound = computed(() => this.res.body()?.maintenance.rounds[0]);
  protected readonly roundRunning = computed(() => {
    const r = this.lastRound();
    return !!r && !r.finishedAt;
  });

  protected duration(r: MaintenanceRound): string {
    const ms = roundDuration(r);
    return ms === undefined ? 'running' : span(ms);
  }
  protected readonly rateTone = rateTone;

  protected resetsIn(at: string): string {
    const due = until(at, this.clock.now());
    return due === 'due' ? 'now' : due;
  }

  protected readonly res = apiResource(getSystem, () => ({}));
  /** GitHub's rate limits come with the status, which follows rateLimit events. */
  private readonly status = inject(StatusService).status;
  protected readonly limits = computed(() => this.status.body()?.githubRateLimits ?? []);

  protected readonly cacheHit = computed(() => {
    const d = this.res.body()?.database;
    return d ? cacheHitRatio(d) : 1;
  });
  private readonly largest = computed(() =>
    Math.max(1, ...(this.res.body()?.tables ?? []).map((t) => t.totalBytes)),
  );
  /** How long this instance has been running, ticking with the clock. */
  protected readonly uptime = computed(() => {
    const started = this.res.body()?.service.startedAt;
    return started ? span(this.clock.now() - Date.parse(started)) : '—';
  });

  constructor() {
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') {
        this.res.reload();
      }
    }, SYSTEM_REFRESH_MS);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
    // Rounds announce themselves; the other figures move slowly, and the timer has them.
    liveReload(() => ({ view: 'system' }), this.res);
  }

  protected percent(v: number): string {
    return `${(v * 100).toFixed(v < 0.1 ? 1 : 0)} %`;
  }

  protected share(n: number): number {
    return (n / this.largest()) * 100;
  }

  protected setting(name: string): string {
    return this.res.body()?.database.settings.find((s) => s.name === name)?.value ?? '—';
  }

  protected lastText(manual?: string, auto?: string): string {
    const l = lastOf(manual, auto);
    return l ? `${ago(l.at, this.clock.now())} (${l.by})` : 'never';
  }

  protected invalidIndexes(t: SystemTable): number {
    return t.indexes.filter((ix) => !ix.valid).length;
  }
}
