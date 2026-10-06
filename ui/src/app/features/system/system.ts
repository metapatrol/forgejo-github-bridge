import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  inject,
  signal,
} from '@angular/core';
import {
  ago,
  bytes,
  count,
  MpClock,
  MpLoadState,
  MpPageHeader,
  MpTime,
  span,
  timestamp,
} from '@metapatrol/ng';
import { getSystem } from '../../api/fn/status/get-system';
import { SystemDatabase } from '../../api/models/system-database';
import { SystemTable } from '../../api/models/system-table';
import { apiResource } from '../../core/api';
import { RefreshService } from '../../core/refresh';
import { Meter } from '../../shared/meter';

/** How often the page reloads on its own: the database has no events, and its figures move slowly. */
export const SYSTEM_REFRESH_MS = 30_000;

/** The later of a manual and an automatic run, and which it was. */
export function lastOf(
  manual: string | undefined,
  auto: string | undefined,
): { at: string; by: 'manual' | 'autovacuum' } | undefined {
  if (!manual && !auto) {
    return undefined;
  }
  if (!auto || (manual && manual > auto)) {
    return { at: manual!, by: 'manual' };
  }
  return { at: auto, by: 'autovacuum' };
}

/** Of all blocks read, the share found in shared buffers; 1 before anything was read. */
export function cacheHitRatio(d: Pick<SystemDatabase, 'blocksHit' | 'blocksRead'>): number {
  const total = d.blocksHit + d.blocksRead;
  return total > 0 ? d.blocksHit / total : 1;
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
  imports: [MpLoadState, MpPageHeader, MpTime, Meter],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './system.html',
})
export class System {
  private readonly clock = inject(MpClock);
  private readonly refresh = inject(RefreshService);
  protected readonly bytes = bytes;
  protected readonly count = count;
  protected readonly ago = ago;
  protected readonly timestamp = timestamp;
  protected readonly interval = interval;
  protected readonly lastOf = lastOf;

  protected readonly res = apiResource(getSystem, () => ({}));
  protected readonly loadedAt = signal(Date.now());

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
      if (!this.refresh.paused() && document.visibilityState === 'visible') {
        this.reload();
      }
    }, SYSTEM_REFRESH_MS);
    inject(DestroyRef).onDestroy(() => clearInterval(timer));
  }

  protected reload(): void {
    this.res.reload();
    this.loadedAt.set(Date.now());
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
