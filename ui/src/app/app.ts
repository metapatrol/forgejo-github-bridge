import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';
import { RouterLink, RouterOutlet } from '@angular/router';
import {
  MpAppShell,
  MpIcon,
  MpNavGroup,
  MpShellAccount,
  MpShellStatus,
  MpTime,
} from '@metapatrol/ng';
import { RefreshService } from './core/refresh';
import { StatusService } from './core/status';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet, RouterLink, MpAppShell, MpShellAccount, MpShellStatus, MpIcon, MpTime],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './app.html',
})
export class App {
  protected readonly status = inject(StatusService);
  protected readonly refresh = inject(RefreshService);

  protected readonly groups = computed<MpNavGroup[]>(() => {
    const failing = this.status.status.body()?.queue.failing ?? 0;
    return [
      {
        label: 'Bridge',
        items: [
          { path: '/', label: 'Overview', icon: 'layout-dashboard', exact: true },
          { path: '/repositories', label: 'Repositories', icon: 'folder-git-2' },
          { path: '/pairs', label: 'Pairs', icon: 'git-compare-arrows' },
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
        ],
      },
    ];
  });
}
