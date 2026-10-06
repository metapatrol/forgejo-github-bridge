import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  numberAttribute,
} from '@angular/core';
import { MpListSearch, MpLoadState, MpPageHeader, mpQueryParams } from '@metapatrol/ng';
import { listPairs } from '../../api/fn/pairs/list-pairs';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { PairKind } from '../../api/models/pair-kind';
import { PairState } from '../../api/models/pair-state';
import { Side } from '../../api/models/side';
import { apiResource } from '../../core/api';
import { autoRefresh } from '../../core/refresh';
import { Pager } from '../../shared/pager';
import { PairsTable } from '../../shared/pairs-table';

const LIMIT = 50;

/** A query parameter as one of the values the API takes, or undefined. */
function oneOf<T extends string>(value: string | undefined, allowed: readonly T[]): T | undefined {
  return allowed.includes(value as T) ? (value as T) : undefined;
}

@Component({
  selector: 'app-pairs',
  imports: [MpListSearch, MpLoadState, MpPageHeader, Pager, PairsTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './pairs.html',
})
export class Pairs {
  // The filters live in the URL, so a filtered list can be linked to.
  readonly repository = input<string>();
  readonly kind = input<string>();
  readonly state = input<string>();
  readonly origin = input<string>();
  readonly q = input<string>();
  readonly offset = input(0, { transform: (v: unknown) => numberAttribute(v, 0) });

  private readonly setQuery = mpQueryParams();
  protected readonly limit = LIMIT;

  protected readonly params = computed(() => ({
    repository: this.repository() || undefined,
    kind: oneOf<PairKind>(this.kind(), ['issue', 'pull']),
    state: oneOf<PairState>(this.state(), ['open', 'closed', 'merged']),
    origin: oneOf<Side>(this.origin(), ['github', 'forgejo']),
    q: this.q() || undefined,
    limit: LIMIT,
    offset: this.offset(),
  }));

  protected readonly pairs = apiResource(listPairs, () => this.params());
  protected readonly repos = apiResource(listRepositories, () => ({}));

  constructor() {
    autoRefresh(this.pairs);
  }

  protected filter(name: string, value: string): void {
    this.setQuery({ [name]: value || null, offset: null });
  }

  protected page(offset: number): void {
    this.setQuery({ offset: offset || null });
  }
}
