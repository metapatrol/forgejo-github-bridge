import { ChangeDetectionStrategy, Component, computed } from '@angular/core';
import { MpLoadState, MpPageHeader } from '@metapatrol/ng';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { autoRefresh } from '../../core/refresh';
import { byHealth, displayNames } from '../../core/repos';
import { REPO_GRID, RepoTile } from '../../shared/repo-tile';

@Component({
  selector: 'app-repositories',
  imports: [MpLoadState, MpPageHeader, RepoTile],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Repositories" [note]="repos.body()?.length?.toString()" />
    <mp-load-state
      [error]="repos.error()"
      [loading]="repos.isLoading() && !repos.hasValue()"
      what="repositories"
    />
    @if (repos.hasValue()) {
      <ul [class]="grid">
        @for (r of sorted(); track r.key) {
          <li class="min-w-0"><app-repo-tile [repo]="r" [name]="name()(r)" detailed /></li>
        } @empty {
          <li class="text-sm text-fg-subtle">No repositories configured.</li>
        }
      </ul>
    }
  `,
})
export class Repositories {
  protected readonly repos = apiResource(listRepositories, () => ({}));
  protected readonly grid = REPO_GRID;
  /** Problems first, like the overview. */
  protected readonly sorted = computed(() => byHealth(this.repos.body() ?? []));
  protected readonly name = computed(() => displayNames(this.repos.body() ?? []));

  constructor() {
    autoRefresh(this.repos);
  }
}
