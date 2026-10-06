import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink, RouterOutlet } from '@angular/router';
import { MpAppShell, MpNavGroup, MpShellStatus } from '@metapatrol/ng';
import { StatusService } from './core/status';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, MpAppShell, MpShellStatus],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  protected readonly status = inject(StatusService);

  protected readonly groups = computed<MpNavGroup[]>(() => {
    const failing = this.status.status.body()?.queue.failing ?? 0;
    return [
      {
        label: 'Bridge',
        items: [
          { path: '/', label: 'Overview', icon: 'layout-dashboard', exact: true },
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
