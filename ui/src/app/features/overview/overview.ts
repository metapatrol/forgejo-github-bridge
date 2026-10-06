import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { count, MpLoadState, MpNotice, MpPageHeader, MpTime } from '@metapatrol/ng';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { repoPath } from '../../core/format';
import { byHealth, lastPoll, repoHealth } from '../../core/repos';
import { autoRefresh } from '../../core/refresh';
import { StatusService } from '../../core/status';

@Component({
  selector: 'app-overview',
  imports: [RouterLink, MpLoadState, MpNotice, MpPageHeader, MpTime],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './overview.html',
})
export class Overview {
  protected readonly status = inject(StatusService).status;
  protected readonly repos = apiResource(listRepositories, () => ({}));
  protected readonly count = count;
  protected readonly repoPath = repoPath;
  protected readonly health = repoHealth;
  protected readonly lastPoll = lastPoll;
  /** Problems first. */
  protected readonly sorted = computed(() => byHealth(this.repos.body() ?? []));

  constructor() {
    autoRefresh(this.repos);
  }

  protected readonly s = computed(() => this.status.body());

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
