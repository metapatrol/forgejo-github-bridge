import { computed, inject, Injectable } from '@angular/core';
import { getStatus } from '../api/fn/status/get-status';
import { apiResource } from './api';
import { EventsService, liveReload } from './events';
import { coreLimit, percentLeft, rateTone } from './rate-limit';

/** The bridge's status, shared by the sidebar and the overview, reloaded as events arrive. */
@Injectable({ providedIn: 'root' })
export class StatusService {
  readonly status = apiResource(getStatus, () => ({}));
  private readonly events = inject(EventsService);

  constructor() {
    liveReload(() => ({ view: 'status' }), this.status);
  }

  /**
   * The bridge's health in one word, for the sidebar. What is worst wins:
   * an API that does not answer, live updates that are interrupted (then
   * nothing else shown can be trusted to be current), no leader, a GitHub
   * rate limit nearly used up, failing entries.
   */
  readonly health = computed(() => {
    if (this.status.error() && !this.status.body()) {
      return { text: 'unreachable', status: 'error', detail: 'the console API does not answer' };
    }
    if (this.events.state() === 'reconnecting') {
      return {
        text: 'reconnecting…',
        status: 'warning',
        detail: 'Live updates are interrupted; what is shown may not be current.',
      };
    }
    const s = this.status.body();
    if (!s) {
      return { text: 'checking', status: null, detail: '' };
    }
    if (!s.leader?.present) {
      return {
        text: 'no leader',
        status: 'error',
        detail: 'No instance holds the leader lock: nothing is being bridged.',
      };
    }
    const core = coreLimit(s.githubRateLimits);
    if (core && rateTone(core) === 'bad') {
      return {
        text: `GitHub ${percentLeft(core)} left`,
        status: 'warning',
        detail: `${core.remaining} of ${core.limit} GitHub API requests left until the window resets.`,
      };
    }
    if (s.queue.failing > 0) {
      return {
        text: `${s.queue.failing} failing`,
        status: 'warning',
        detail: `${s.queue.failing} queued ${s.queue.failing === 1 ? 'entry' : 'entries'} failed their last attempt.`,
      };
    }
    return {
      text: 'bridging',
      status: 'success',
      detail: 'An instance is doing the work; nothing fails.',
    };
  });
}
