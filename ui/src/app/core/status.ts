import { computed, Injectable } from '@angular/core';
import { getStatus } from '../api/fn/status/get-status';
import { apiResource } from './api';
import { liveReload } from './events';

/** The bridge's status, shared by the sidebar and the overview, refreshed with the console. */
@Injectable({ providedIn: 'root' })
export class StatusService {
  readonly status = apiResource(getStatus, () => ({}));

  constructor() {
    liveReload(() => ({ view: 'status' }), this.status);
  }

  /** The bridge's health in one word, for the sidebar. */
  readonly health = computed(() => {
    if (this.status.error() && !this.status.body()) {
      return { text: 'unreachable', status: 'error', detail: 'the console API does not answer' };
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
