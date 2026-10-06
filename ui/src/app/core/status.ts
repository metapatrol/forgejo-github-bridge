import { computed, inject, Injectable } from '@angular/core';
import { getStatus } from '../api/fn/status/get-status';
import { apiResource } from './api';
import { EventsService, liveReload } from './events';

/** The bridge's status, shared by the sidebar and the overview, reloaded as events arrive. */
@Injectable({ providedIn: 'root' })
export class StatusService {
  readonly status = apiResource(getStatus, () => ({}));
  private readonly events = inject(EventsService);

  constructor() {
    liveReload(() => ({ view: 'status' }), this.status);
  }

  /**
   * The console's own state in one word, for the sidebar: whether what it
   * shows can be trusted to be current. The bridge's state (leader, queue,
   * GitHub) is on the pages themselves.
   */
  readonly connection = computed(() => {
    if (this.status.error()) {
      return {
        text: 'unreachable',
        status: 'error',
        detail: 'The console API does not answer.',
      };
    }
    switch (this.events.state()) {
      case 'reconnecting':
        return {
          text: 'reconnecting…',
          status: 'warning',
          detail: 'Live updates are interrupted; what is shown may not be current.',
        };
      case 'live':
        return {
          text: 'connected',
          status: 'success',
          detail: 'Live updates are on; what is shown is current.',
        };
      default:
        return { text: 'connecting…', status: 'idle', detail: 'Opening the live updates.' };
    }
  });
}
