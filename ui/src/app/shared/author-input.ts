import { ChangeDetectionStrategy, Component, computed, inject, input, output } from '@angular/core';
import { Api } from '../api/api';
import { listAuthors } from '../api/fn/pairs/list-authors';
import { PairKind } from '../api/models/pair-kind';
import { Side } from '../api/models/side';
import { sideName } from '../core/history';
import { MAX_SUGGESTIONS, Suggestion, SuggestInput } from './suggest-input';

/** An author filter: a login, and the side it is on. */
export interface AuthorFilter {
  author: string;
  origin: Side;
}

/**
 * An author filter for a list of issues or pull requests that suggests the
 * people who opened one as the user types, searched by the API
 * (GET /authors?kind=…&q=): the login, its side muted beside it. Pull
 * requests come from GitHub only, so only GitHub logins are suggested there.
 */
@Component({
  selector: 'app-author-input',
  imports: [SuggestInput],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block' },
  template: `
    <app-suggest-input
      label="Opened by"
      placeholder="anyone"
      clearLabel="Remove the author filter"
      [value]="author()"
      [badge]="author() ? badge() : ''"
      [search]="search"
      (picked)="pick($event)"
      (cleared)="committed.emit(null)"
    />
  `,
})
export class AuthorInput {
  readonly kind = input.required<PairKind>();
  /** The current filter; empty for anyone. */
  readonly author = input<string | undefined>('');
  readonly origin = input<Side | undefined>();
  /** The new filter, or null for anyone. */
  readonly committed = output<AuthorFilter | null>();

  private readonly api = inject(Api);
  protected readonly badge = computed(() => sideName(this.origin() ?? 'github'));

  protected readonly search = async (q: string): Promise<Suggestion<Side>[]> => {
    const page = await this.api.invoke(listAuthors, {
      kind: this.kind(),
      side: this.kind() === 'pull' ? 'github' : undefined,
      q: q || undefined,
      limit: MAX_SUGGESTIONS,
    });
    return page.items.map((a) => ({
      value: a.login,
      label: a.login,
      hint: sideName(a.side),
      data: a.side,
    }));
  };

  protected pick(s: Suggestion): void {
    const origin = (s.data as Side | undefined) ?? 'github';
    if (s.value !== this.author() || origin !== this.origin()) {
      this.committed.emit({ author: s.value, origin });
    }
  }
}
