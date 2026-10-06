import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { count, MpClock, MpLoadState, MpNotice, MpPageHeader, MpTime } from '@metapatrol/ng';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { repoPath } from '../../core/format';
import { byHealth, displayNames } from '../../core/repos';
import { REPO_GRID, RepoTile, TILE_LINK } from '../../shared/repo-tile';
import { liveReload } from '../../core/events';
import { OPEN } from '../../core/state-filter';
import { coreLimit, percentLeft, rateTone } from '../../core/rate-limit';
import { until } from '../../core/format';
import { Meter } from '../../shared/meter';
import { StatusService } from '../../core/status';

@Component({
  selector: 'app-overview',
  imports: [RouterLink, MpLoadState, MpNotice, MpPageHeader, MpTime, Meter, RepoTile],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview.html',
})
export class Overview {
  protected readonly status = inject(StatusService).status;
  protected readonly repos = apiResource(listRepositories, () => ({}));
  protected readonly count = count;
  protected readonly repoPath = repoPath;
  protected readonly grid = REPO_GRID;
  protected readonly tileLink = TILE_LINK;
  protected readonly open = OPEN;
  /** Problems first. */
  protected readonly sorted = computed(() => byHealth(this.repos.body() ?? []));
  /** Repository names for the tiles: without the owner when they share one. */
  protected readonly name = computed(() => displayNames(this.repos.body() ?? []));

  constructor() {
    liveReload(() => ({ view: 'overview' }), this.repos);
  }

  protected readonly s = computed(() => this.status.body());
  /** GitHub's core window, as the working instance last saw it. */
  protected readonly core = computed(() => coreLimit(this.s()?.githubRateLimits));
  protected readonly coreTone = computed(() => {
    const c = this.core();
    return c ? rateTone(c) : 'ok';
  });
  protected readonly percentLeft = percentLeft;
  private readonly clock = inject(MpClock);
  protected resetsIn(at: string): string {
    const due = until(at, this.clock.now());
    return due === 'due' ? 'resets now' : `resets ${due}`;
  }
  protected readonly working = computed(() => !!this.s()?.leader?.present);
  protected readonly leaderHost = computed(() => this.s()?.leader?.instance || 'an instance');
  protected readonly leaderSince = computed(() => this.s()?.leader?.connectedAt);

  /**
   * The muted line of the bridge tile: the version, this instance where it
   * is not the one working, and its mode.
   */
  protected readonly instanceLine = computed(() => {
    const s = this.s();
    if (!s) {
      return '';
    }
    const parts = [s.version];
    if (s.instance !== s.leader?.instance) {
      parts.push(s.instance);
    }
    parts.push(`${s.mode} mode`);
    return parts.join(' · ');
  });

  protected readonly buildTip = computed(() => {
    const s = this.s();
    return s && s.commit !== 'unknown' ? `${s.commit}, built ${s.builtAt}` : 'local build';
  });

  /** Sums over every repository. */
  protected readonly totals = computed(() => {
    const t = { issuesOpen: 0, issuesClosed: 0, pullsOpen: 0, pullsMerged: 0, pullsClosed: 0 };
    for (const r of this.repos.body() ?? []) {
      t.issuesOpen += r.counts.issuesOpen;
      t.issuesClosed += r.counts.issuesClosed;
      t.pullsOpen += r.counts.pullsOpen;
      t.pullsMerged += r.counts.pullsMerged;
      t.pullsClosed += r.counts.pullsClosed;
    }
    return t;
  });
}
