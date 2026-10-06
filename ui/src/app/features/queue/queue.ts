import {
  booleanAttribute,
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
} from '@angular/core';
import { MpListSearch, MpLoadState, MpPageHeader, mpQueryParams } from '@metapatrol/ng';
import { ListQueue$Params, listQueue } from '../../api/fn/queue/list-queue';
import { pagedList, sortParam } from '../../core/paged';
import { liveReload } from '../../core/events';
import { QUEUE_SORTS, QueueTable } from '../../shared/queue-table';
import { RepositoryInput } from '../../shared/repository-input';

@Component({
  selector: 'app-queue',
  imports: [MpListSearch, MpLoadState, MpPageHeader, QueueTable, RepositoryInput],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Queue" [note]="queue.total().toString()" />
    <p class="mb-4 max-w-3xl text-sm text-fg-muted">
      What waits to be reconciled. An entry that fails is retried with backoff, from 30 seconds up
      to 30 minutes; the ones failing come first.
    </p>
    <section class="mp-panel mp-panel-flush">
      <div class="mp-panel-head flex-wrap items-center gap-3">
        <mp-list-search
          class="flex-nowrap"
          placeholder="Number, repository or error"
          [query]="q()"
          [shown]="queue.items().length"
          [total]="queue.total()"
        />
        <label class="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            [checked]="failing()"
            (change)="setQuery({ failing: $any($event.target).checked || null })"
          />
          failing only
        </label>
        <app-repository-input
          class="w-72"
          [value]="repository()"
          (committed)="setQuery({ repository: $event || null })"
        />
      </div>
      <mp-load-state
        [error]="queue.error()"
        [loading]="queue.loading() && !queue.loaded()"
        what="queue"
      />
      @if (queue.loaded()) {
        <app-queue-table
          [entries]="queue.items()"
          [showRepository]="!repository()"
          [empty]="failing() ? 'Nothing is failing.' : 'Nothing is queued: everything is in step.'"
          sortable
          [sort]="sort()"
          [more]="queue.more()"
          (nearEnd)="queue.loadMore()"
        />
      }
    </section>
  `,
})
export class Queue {
  readonly failing = input(false, { transform: booleanAttribute });
  readonly repository = input<string>();
  readonly q = input<string>();
  /** ?sort=: a column, descending with a leading -; failing first, then by when due, by default. */
  readonly sort = input<string>();
  protected readonly setQuery = mpQueryParams();

  protected readonly params = computed(() => ({
    failing: this.failing() || undefined,
    repository: this.repository() || undefined,
    q: this.q() || undefined,
    sort: sortParam<NonNullable<ListQueue$Params['sort']>>(this.sort(), QUEUE_SORTS),
  }));
  protected readonly queue = pagedList(listQueue, () => this.params());

  constructor() {
    liveReload(() => ({ view: 'queue' }), this.queue);
  }
}
