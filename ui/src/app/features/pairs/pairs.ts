import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { MpLoadState, MpPageHeader } from '@metapatrol/ng';
import { ListPairs$Params, listPairs } from '../../api/fn/pairs/list-pairs';
import { pagedList, sortParam } from '../../core/paged';
import { PairKind } from '../../api/models/pair-kind';
import { apiState } from '../../core/state-filter';
import { Side } from '../../api/models/side';
import { KINDS } from '../../core/kinds';
import { liveReload } from '../../core/events';
import { FilterOption, PairFilters } from '../../shared/pair-filters';
import { PAIRS_SORTS, PairsTable } from '../../shared/pairs-table';

const STATES: Record<PairKind, FilterOption[]> = {
  issue: [
    { value: 'open', label: 'open' },
    { value: 'closed', label: 'closed' },
  ],
  pull: [
    { value: 'open', label: 'open' },
    { value: 'merged', label: 'merged' },
    { value: 'closed', label: 'closed unmerged' },
  ],
};

const ORIGINS: FilterOption[] = [
  { value: 'github', label: 'opened on GitHub' },
  { value: 'forgejo', label: 'opened in Forgejo' },
];

/** A query parameter as one of the values the API takes, or undefined. */
function oneOf<T extends string>(value: string | undefined, allowed: readonly T[]): T | undefined {
  return allowed.includes(value as T) ? (value as T) : undefined;
}

/** The list of issues, or of pull requests: the route's data says which. */
@Component({
  selector: 'app-pairs',
  imports: [MpLoadState, MpPageHeader, PairFilters, PairsTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header [title]="title()" [note]="pairs.total().toString()" />
    <section class="mp-panel mp-panel-flush">
      <app-pair-filters
        [kind]="kind()"
        [author]="author()"
        [states]="states()"
        [origins]="kind() === 'issue' ? origins : []"
        [repository]="repository()"
        [state]="state()"
        [origin]="params().origin"
        [q]="q()"
        [shown]="pairs.items().length"
        [total]="pairs.total()"
      />
      <mp-load-state
        [error]="pairs.error()"
        [loading]="pairs.loading() && !pairs.loaded()"
        [what]="noun()"
      />
      @if (pairs.loaded()) {
        <app-pairs-table
          [kind]="kind()"
          [pairs]="pairs.items()"
          [showRepository]="!repository()"
          [showAuthor]="!author()"
          [empty]="'No ' + noun() + ' match.'"
          sortable
          [sort]="sort()"
          [more]="pairs.more()"
          (nearEnd)="pairs.loadMore()"
        />
      }
    </section>
  `,
})
export class Pairs {
  /** From the route. */
  readonly kind = input.required<PairKind>();
  // The filters live in the URL, so a filtered list can be linked to.
  readonly repository = input<string>();
  readonly state = input<string>();
  readonly origin = input<string>();
  readonly q = input<string>();
  /** Who opened them, with origin the side that login is on. */
  readonly author = input<string>();
  /** ?sort=: a column, descending with a leading -; the newest first by default. */
  readonly sort = input<string>();

  protected readonly origins = ORIGINS;
  protected readonly noun = computed(() => KINDS[this.kind()].many);
  protected readonly title = computed(() => {
    const n = this.noun();
    return n[0].toUpperCase() + n.slice(1);
  });
  protected readonly states = computed(() => STATES[this.kind()]);

  protected readonly params = computed(() => ({
    kind: this.kind(),
    repository: this.repository() || undefined,
    state: apiState(this.state()),
    origin:
      this.kind() === 'issue' || this.author()
        ? oneOf<Side>(this.origin(), ['github', 'forgejo'])
        : undefined,
    author: this.author() || undefined,
    q: this.q() || undefined,
    sort: sortParam<NonNullable<ListPairs$Params['sort']>>(this.sort(), PAIRS_SORTS),
  }));

  protected readonly pairs = pagedList(listPairs, () => this.params());

  constructor() {
    liveReload(
      () => ({ view: 'pairs', kind: this.kind(), repository: this.repository() || undefined }),
      this.pairs,
    );
  }
}
