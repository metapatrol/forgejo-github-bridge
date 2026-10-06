import {
  booleanAttribute,
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
} from '@angular/core';
import { MpLoadState, MpPageHeader, mpQueryParams } from '@metapatrol/ng';
import { listQueue } from '../../api/fn/queue/list-queue';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { liveReload } from '../../core/events';
import { QueueTable } from '../../shared/queue-table';

@Component({
  selector: 'app-queue',
  imports: [MpLoadState, MpPageHeader, QueueTable],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Queue" [note]="queue.body()?.length?.toString()" />
    <p class="mb-4 max-w-3xl text-sm text-fg-muted">
      What waits to be reconciled. An entry that fails is retried with backoff, from 30 seconds up
      to 30 minutes; the ones failing come first.
    </p>
    <section class="mp-panel mp-panel-flush">
      <div class="mp-panel-head flex-wrap items-center gap-3">
        <label class="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            [checked]="failing()"
            (change)="setQuery({ failing: $any($event.target).checked || null })"
          />
          failing only
        </label>
        <label class="sr-only" for="q-repository">Repository</label>
        <select
          id="q-repository"
          class="mp-input w-auto"
          (change)="setQuery({ repository: $any($event.target).value || null })"
        >
          <option value="">all repositories</option>
          @for (r of repos.body() ?? []; track r.key) {
            <option [value]="r.key" [selected]="r.key === repository()">{{ r.key }}</option>
          }
        </select>
      </div>
      <mp-load-state
        [error]="queue.error()"
        [loading]="queue.isLoading() && !queue.hasValue()"
        what="queue"
      />
      @if (queue.hasValue()) {
        <app-queue-table
          [entries]="queue.body() ?? []"
          [showRepository]="!repository()"
          [empty]="failing() ? 'Nothing is failing.' : 'Nothing is queued: everything is in step.'"
        />
      }
    </section>
  `,
})
export class Queue {
  readonly failing = input(false, { transform: booleanAttribute });
  readonly repository = input<string>();
  protected readonly setQuery = mpQueryParams();

  protected readonly params = computed(() => ({
    failing: this.failing() || undefined,
    repository: this.repository() || undefined,
  }));
  protected readonly queue = apiResource(listQueue, () => this.params());
  protected readonly repos = apiResource(listRepositories, () => ({}));

  constructor() {
    liveReload(() => ({ view: 'queue' }), this.queue);
  }
}
