import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import {
  MpLoadState,
  MpPageHeader,
  MpPanel,
  MpPanelActions,
  MpTime,
  timestamp,
} from '@metapatrol/ng';
import { listPairs } from '../../api/fn/pairs/list-pairs';
import { listQueue } from '../../api/fn/queue/list-queue';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { liveReload } from '../../core/events';
import { ANY_STATE, OPEN } from '../../core/state-filter';
import { SideLabel } from '../../shared/badges';
import { PairsTable } from '../../shared/pairs-table';
import { QueueTable } from '../../shared/queue-table';

const RECENT = 10;

@Component({
  selector: 'app-repository',
  imports: [
    RouterLink,
    MpLoadState,
    MpPageHeader,
    MpPanel,
    MpPanelActions,
    MpTime,
    SideLabel,
    PairsTable,
    QueueTable,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './repository.html',
})
export class RepositoryDetail {
  readonly owner = input.required<string>();
  readonly name = input.required<string>();
  protected readonly key = computed(() => `${this.owner()}/${this.name()}`);
  protected readonly timestamp = timestamp;

  protected readonly repos = apiResource(listRepositories, () => ({}));
  protected readonly repo = computed(() => this.repos.body()?.find((r) => r.key === this.key()));
  protected readonly missing = computed(() => this.repos.hasValue() && !this.repo());

  protected readonly queue = apiResource(listQueue, () => ({ repository: this.key() }));
  protected readonly issues = apiResource(listPairs, () => ({
    repository: this.key(),
    kind: 'issue' as const,
    state: 'open' as const,
    limit: RECENT,
  }));
  protected readonly pulls = apiResource(listPairs, () =>
    this.repo()?.pullRequests
      ? { repository: this.key(), kind: 'pull' as const, state: 'open' as const, limit: RECENT }
      : undefined,
  );

  constructor() {
    liveReload(
      () => ({ view: 'repository', repository: this.key() }),
      this.repos,
      this.queue,
      this.issues,
      this.pulls,
    );
  }

  /** The list behind a panel: its open ones, or all of them when none is open. */
  protected openLink(open: number | undefined): Record<string, string> {
    return { repository: this.key(), state: open === 0 ? ANY_STATE : OPEN.state };
  }

  protected moreLabel(open: number | undefined): string {
    if (open === undefined) {
      return '';
    }
    return open === 0 ? 'all →' : `${open} open →`;
  }
}
