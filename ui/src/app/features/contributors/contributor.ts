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
import { listAuthors } from '../../api/fn/pairs/list-authors';
import { listPairs } from '../../api/fn/pairs/list-pairs';
import { Side } from '../../api/models/side';
import { apiResource } from '../../core/api';
import { liveReload } from '../../core/events';
import { contributorLinks } from '../../core/links';
import { QUIET_LINK } from '../../shared/tile';
import { sideName } from '../../core/history';
import { PairsTable } from '../../shared/pairs-table';

const RECENT = 10;

/** One contributor: what the bridge holds from them. */
@Component({
  selector: 'app-contributor',
  imports: [RouterLink, MpLoadState, MpPageHeader, MpPanel, MpPanelActions, MpTime, PairsTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './contributor.html',
})
export class ContributorDetail {
  /** From the route. */
  readonly side = input.required<Side>();
  readonly login = input.required<string>();
  protected readonly sideName = sideName;
  protected readonly timestamp = timestamp;

  protected readonly authors = apiResource(listAuthors, () => ({}));
  protected readonly contributor = computed(() =>
    this.authors.body()?.items.find((a) => a.side === this.side() && a.login === this.login()),
  );
  protected readonly unknown = computed(() => this.authors.hasValue() && !this.contributor());

  protected readonly issues = apiResource(listPairs, () => ({
    kind: 'issue' as const,
    author: this.login(),
    origin: this.side(),
    sort: '-created' as const,
    limit: RECENT,
  }));
  /** Pull requests come from GitHub only. */
  protected readonly pulls = apiResource(listPairs, () =>
    this.side() === 'github'
      ? {
          kind: 'pull' as const,
          author: this.login(),
          origin: this.side(),
          sort: '-created' as const,
          limit: RECENT,
        }
      : undefined,
  );

  /** Where the counts and the panels' "all →" go: their pairs, every state. */
  protected readonly links = computed(() => contributorLinks(this.login(), this.side()));
  protected readonly quiet = QUIET_LINK;

  constructor() {
    liveReload(
      () => ({ view: 'contributor', login: this.login(), side: this.side() }),
      this.authors,
      this.issues,
      this.pulls,
    );
  }
}
