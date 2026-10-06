import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  count,
  MpClock,
  MpListSearch,
  MpLoadState,
  MpNotice,
  MpPageHeader,
  MpTime,
} from '@metapatrol/ng';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { listAuthors } from '../../api/fn/pairs/list-authors';
import { contributorLink } from '../../shared/author';
import { sideName } from '../../core/history';
import { apiResource } from '../../core/api';
import { repoPath } from '../../core/format';
import { displayNames, FILTER_FROM } from '../../core/repos';
import { REPO_GRID, RepoTile, TILE_LINK } from '../../shared/repo-tile';
import { liveReload } from '../../core/events';
import { OPEN } from '../../core/state-filter';
import { coreLimit, percentLeft, rateTone } from '../../core/rate-limit';
import { until } from '../../core/format';
import { Meter } from '../../shared/meter';
import { StatusService } from '../../core/status';

/** How many contributors the overview names. */
const TOP_AUTHORS = 8;

@Component({
  selector: 'app-overview',
  imports: [RouterLink, MpListSearch, MpLoadState, MpNotice, MpPageHeader, MpTime, Meter, RepoTile],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview.html',
})
export class Overview {
  protected readonly status = inject(StatusService).status;
  /** Every repository, problems first: the API's default order. */
  protected readonly repos = apiResource(listRepositories, () => ({}));
  /** The most active contributors, as the API orders them. */
  protected readonly authors = apiResource(listAuthors, () => ({ limit: TOP_AUTHORS }));
  protected readonly topAuthors = computed(() => this.authors.body()?.items ?? []);
  protected readonly contributorLink = contributorLink;
  protected readonly sideName = sideName;
  protected readonly count = count;
  protected readonly repoPath = repoPath;
  protected readonly grid = REPO_GRID;
  protected readonly tileLink = TILE_LINK;
  protected readonly open = OPEN;
  /** The filter above the tiles, from ?repo=; offered only for many repositories. */
  readonly repo = input<string>();
  protected readonly all = computed(() => this.repos.body()?.items ?? []);
  protected readonly filterable = computed(() => this.all().length >= FILTER_FROM);
  /** The same, searched by the API, while there is a filter. */
  protected readonly matching = apiResource(listRepositories, () =>
    this.filterable() && this.repo() ? { q: this.repo() } : undefined,
  );
  /** The tiles: every repository, or the ones the filter matches; in the API's order. */
  protected readonly sorted = computed(() =>
    this.filterable() && this.repo() ? (this.matching.body()?.items ?? []) : this.all(),
  );
  /** Repository names for the tiles: without the owner when they share one. */
  protected readonly name = computed(() => displayNames(this.all()));

  constructor() {
    liveReload(() => ({ view: 'overview' }), this.repos, this.matching, this.authors);
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

  /** Sums over every repository. */
  protected readonly totals = computed(() => {
    const t = { issuesOpen: 0, issuesClosed: 0, pullsOpen: 0, pullsMerged: 0, pullsClosed: 0 };
    for (const r of this.all()) {
      t.issuesOpen += r.counts.issuesOpen;
      t.issuesClosed += r.counts.issuesClosed;
      t.pullsOpen += r.counts.pullsOpen;
      t.pullsMerged += r.counts.pullsMerged;
      t.pullsClosed += r.counts.pullsClosed;
    }
    return t;
  });
}
