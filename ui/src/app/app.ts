import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink, RouterOutlet } from '@angular/router';
import { MpAppShell, MpNavGroup, MpShellAccount, MpShellStatus } from '@metapatrol/ng';
import { EventsService } from './core/events';
import { StatusService } from './core/status';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, MpAppShell, MpShellAccount, MpShellStatus],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  protected readonly status = inject(StatusService);
  protected readonly events = inject(EventsService);

  /** The stream in a word, with the dot that goes with it. Passive: there is nothing to switch. */
  protected readonly live = computed(() => {
    switch (this.events.state()) {
      case 'live':
        return { text: 'live', status: 'success', tip: 'Following changes as they happen.' };
      case 'reconnecting':
        return {
          text: 'reconnecting…',
          status: 'warning',
          tip: 'The event stream dropped. What you see may not be current until it is back.',
        };
      default:
        return { text: 'connecting…', status: 'idle', tip: 'Opening the event stream.' };
    }
  });

  constructor() {
    // The stream opens with the console, not with the first page that listens.
    this.events.on(() => undefined);
  }

  protected readonly groups = computed<MpNavGroup[]>(() => {
    const failing = this.status.status.body()?.queue.failing ?? 0;
    return [
      {
        label: 'Bridge',
        items: [
          { path: '/', label: 'Overview', icon: 'layout-dashboard', exact: true },
          { path: '/repositories', label: 'Repositories', icon: 'folder-git-2' },
          { path: '/issues', label: 'Issues', icon: 'circle-dot' },
          { path: '/pulls', label: 'Pull requests', icon: 'git-pull-request' },
        ],
      },
      {
        label: 'Operations',
        items: [
          {
            path: '/queue',
            label: 'Queue',
            icon: 'inbox',
            ...(failing > 0
              ? {
                  badge: {
                    count: failing,
                    tone: 'bad' as const,
                    label: 'failing',
                    tip: `${failing} failing`,
                  },
                }
              : {}),
          },
          { path: '/system', label: 'System', icon: 'database' },
        ],
      },
    ];
  });
}
