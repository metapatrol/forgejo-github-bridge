import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { MpListSearch, mpQueryParams } from '@metapatrol/ng';
import { RepositoryInput } from './repository-input';
import { AuthorFilter, AuthorInput } from './author-input';
import { PairKind } from '../api/models/pair-kind';

export interface FilterOption {
  value: string;
  label: string;
}

/** The filters above a list of issues or pull requests, all in the URL. */
@Component({
  selector: 'app-pair-filters',
  imports: [MpListSearch, RepositoryInput, AuthorInput],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'mp-panel-head flex-wrap items-center gap-3' },
  template: `
    <mp-list-search
      class="flex-nowrap"
      [query]="q()"
      [shown]="shown()"
      [total]="total()"
      placeholder="Title or number"
    />
    <div class="flex flex-wrap items-center gap-2">
      <app-repository-input
        class="w-72"
        [value]="repository()"
        [pullsOnly]="kind() === 'pull'"
        (committed)="filter('repository', $event)"
      />
      <app-author-input
        class="w-56"
        [kind]="kind()"
        [author]="author()"
        [origin]="origin() === 'forgejo' ? 'forgejo' : 'github'"
        (committed)="setAuthor($event)"
      />
      <label class="sr-only" for="f-state">State</label>
      <select id="f-state" class="mp-input w-auto" (change)="setState($any($event.target).value)">
        @for (s of states(); track s.value) {
          <option [value]="s.value" [selected]="s.value === state()">{{ s.label }}</option>
        }
        <option value="any" [selected]="state() === 'any'">any state</option>
      </select>
      @if (origins().length) {
        <label class="sr-only" for="f-origin">Origin</label>
        <select
          id="f-origin"
          class="mp-input w-auto"
          (change)="filter('origin', $any($event.target).value)"
        >
          <option value="">from either side</option>
          @for (o of origins(); track o.value) {
            <option [value]="o.value" [selected]="o.value === origin()">{{ o.label }}</option>
          }
        </select>
      }
    </div>
  `,
})
export class PairFilters {
  readonly kind = input.required<PairKind>();
  /** Who opened them; the origin says on which side. */
  readonly author = input<string | undefined>();
  readonly states = input<readonly FilterOption[]>([]);
  /** Empty: no origin filter. */
  readonly origins = input<readonly FilterOption[]>([]);
  readonly repository = input<string | undefined>();
  /** The selected state: one of states(), or 'any'. */
  readonly state = input<string | undefined>();
  readonly origin = input<string | undefined>();
  readonly q = input<string | undefined>();
  readonly shown = input(0);
  readonly total = input(0);
  private readonly setQuery = mpQueryParams();

  protected setState(selected: string): void {
    this.setQuery({ state: selected });
  }

  /** An author comes with the side their login is on; clearing removes both. */
  protected setAuthor(f: AuthorFilter | null): void {
    this.setQuery({ author: f?.author ?? null, origin: f?.origin ?? null });
  }

  protected filter(name: string, value: string): void {
    this.setQuery({ [name]: value || null });
  }
}
