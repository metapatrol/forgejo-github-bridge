import {
  booleanAttribute,
  ChangeDetectionStrategy,
  Component,
  inject,
  input,
  output,
} from '@angular/core';
import { Api } from '../api/api';
import { listRepositories } from '../api/fn/repositories/list-repositories';
import { MAX_SUGGESTIONS, Suggestion, SuggestInput } from './suggest-input';

/**
 * A repository filter that suggests repositories as the user types, searched
 * by the API (GET /repositories?q=): the GitHub key, the Forgejo name muted
 * beside it.
 */
@Component({
  selector: 'app-repository-input',
  imports: [SuggestInput],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block' },
  template: `
    <app-suggest-input
      label="Repository"
      placeholder="all repositories"
      clearLabel="Remove the repository filter"
      [value]="value()"
      [search]="search"
      (picked)="commit($event.value)"
      (cleared)="commit('')"
    />
  `,
})
export class RepositoryInput {
  /** The current filter, a GitHub key; empty for all repositories. */
  readonly value = input<string | undefined>('');
  /** Suggest only repositories that import pull requests. */
  readonly pullsOnly = input(false, { transform: booleanAttribute });
  /** The new filter: a key, or '' for all repositories. */
  readonly committed = output<string>();

  private readonly api = inject(Api);

  protected readonly search = async (q: string): Promise<Suggestion[]> => {
    const page = await this.api.invoke(listRepositories, {
      q: q || undefined,
      sort: 'name',
      pullRequests: this.pullsOnly() || undefined,
      limit: MAX_SUGGESTIONS,
    });
    return page.items.map((r) => ({ value: r.key, label: r.key, hint: r.forgejo }));
  };

  protected commit(key: string): void {
    if (key !== (this.value() ?? '')) {
      this.committed.emit(key);
    }
  }
}
