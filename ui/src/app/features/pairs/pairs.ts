import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { MpIcon, MpLoadState, MpPageHeader, mpQueryParams } from '@metapatrol/ng';
import { RouterLink } from '@angular/router';
import { ListPairs$Params, listPairs } from '../../api/fn/pairs/list-pairs';
import { pagedList, sortParam } from '../../core/paged';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { PairKind } from '../../api/models/pair-kind';
import { apiState } from '../../core/state-filter';
import { Side } from '../../api/models/side';
import { apiResource } from '../../core/api';
import { KINDS } from '../../core/kinds';
import { liveReload } from '../../core/events';
import { FilterOption, PairFilters } from '../../shared/pair-filters';
import { PAIRS_SORTS, PairsTable } from '../../shared/pairs-table';
import { contributorLink } from '../../shared/author';

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
  imports: [RouterLink, MpIcon, MpLoadState, MpPageHeader, PairFilters, PairsTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header [title]="title()" [note]="pairs.total().toString()" />
    <section class="mp-panel mp-panel-flush">
      <app-pair-filters
        [repositories]="repositories()"
        [states]="states()"
        [origins]="kind() === 'issue' ? origins : []"
        [repository]="repository()"
        [state]="state()"
        [origin]="params().origin"
        [q]="q()"
        [shown]="pairs.items().length"
        [total]="pairs.total()"
      />
      @if (author()) {
        <div class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-2 text-xs">
          <span class="text-fg-subtle">opened by</span>
          <span
            class="inline-flex items-center gap-1 rounded-full border border-line bg-surface-2 py-0.5 pr-1 pl-2.5"
          >
            <a [routerLink]="contributorLink()" class="mp-link font-mono">{{ author() }}</a>
            <span class="text-fg-subtle">· {{ originName() }}</span>
            <button
              type="button"
              class="grid size-5 place-items-center rounded-full text-fg-muted hover:bg-surface hover:text-fg"
              [attr.aria-label]="'Remove the filter by ' + author()"
              (click)="clearAuthor()"
            >
              <mp-icon name="x" class="size-3" />
            </button>
          </span>
        </div>
      }
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

  private readonly setQuery = mpQueryParams();
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
  protected readonly repos = apiResource(listRepositories, () => ({}));
  /** Pull requests only come from repositories that import them. */
  protected readonly repositories = computed(() =>
    (this.repos.body()?.items ?? [])
      .filter((r) => this.kind() === 'issue' || r.pullRequests)
      .map((r) => r.key),
  );

  constructor() {
    liveReload(
      () => ({ view: 'pairs', kind: this.kind(), repository: this.repository() || undefined }),
      this.pairs,
    );
  }

  /** The side an author filter's login is on: GitHub unless the URL says Forgejo. */
  private readonly authorSide = computed<Side>(() =>
    this.origin() === 'forgejo' ? 'forgejo' : 'github',
  );
  protected readonly originName = computed(() =>
    this.authorSide() === 'github' ? 'GitHub' : 'Forgejo',
  );
  protected readonly contributorLink = computed(() =>
    contributorLink(this.authorSide(), this.author() ?? ''),
  );

  protected clearAuthor(): void {
    // The origin came with the author on a pull request list, which has no origin filter of its own.
    this.setQuery({
      author: null,
      ...(this.kind() === 'pull' ? { origin: null } : {}),
    });
  }
}
