import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  numberAttribute,
} from '@angular/core';
import { MpLoadState, MpPageHeader, mpQueryParams } from '@metapatrol/ng';
import { listPairs } from '../../api/fn/pairs/list-pairs';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { PairKind } from '../../api/models/pair-kind';
import { PairState } from '../../api/models/pair-state';
import { Side } from '../../api/models/side';
import { apiResource } from '../../core/api';
import { KINDS } from '../../core/kinds';
import { autoRefresh } from '../../core/refresh';
import { Pager } from '../../shared/pager';
import { FilterOption, PairFilters } from '../../shared/pair-filters';
import { PairsTable } from '../../shared/pairs-table';

const LIMIT = 50;

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
  imports: [MpLoadState, MpPageHeader, Pager, PairFilters, PairsTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header [title]="title()" [note]="pairs.body()?.total?.toString()" />
    <section class="mp-panel mp-panel-flush">
      <app-pair-filters
        [repositories]="repositories()"
        [states]="states()"
        [origins]="kind() === 'issue' ? origins : []"
        [repository]="repository()"
        [state]="params().state"
        [origin]="params().origin"
        [q]="q()"
        [shown]="pairs.body()?.items?.length ?? 0"
        [total]="pairs.body()?.total ?? 0"
      />
      <mp-load-state
        [error]="pairs.error()"
        [loading]="pairs.isLoading() && !pairs.hasValue()"
        [what]="noun()"
      />
      @if (pairs.hasValue()) {
        <app-pairs-table
          [kind]="kind()"
          [pairs]="pairs.body()?.items ?? []"
          [showRepository]="!repository()"
          [empty]="'No ' + noun() + ' match.'"
        />
        <div class="flex justify-end border-t border-line px-4 py-2">
          <app-pager
            [offset]="offset()"
            [limit]="limit"
            [shown]="pairs.body()?.items?.length ?? 0"
            [total]="pairs.body()?.total ?? 0"
            (moved)="page($event)"
          />
        </div>
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
  readonly offset = input(0, { transform: (v: unknown) => numberAttribute(v, 0) });

  private readonly setQuery = mpQueryParams();
  protected readonly limit = LIMIT;
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
    state: oneOf<PairState>(
      this.state(),
      this.states().map((s) => s.value as PairState),
    ),
    origin: this.kind() === 'issue' ? oneOf<Side>(this.origin(), ['github', 'forgejo']) : undefined,
    q: this.q() || undefined,
    limit: LIMIT,
    offset: this.offset(),
  }));

  protected readonly pairs = apiResource(listPairs, () => this.params());
  protected readonly repos = apiResource(listRepositories, () => ({}));
  /** Pull requests only come from repositories that import them. */
  protected readonly repositories = computed(() =>
    (this.repos.body() ?? [])
      .filter((r) => this.kind() === 'issue' || r.pullRequests)
      .map((r) => r.key),
  );

  constructor() {
    autoRefresh(this.pairs);
  }

  protected page(offset: number): void {
    this.setQuery({ offset: offset || null });
  }
}
