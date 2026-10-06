import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpLoadState, MpPageHeader, MpTime } from '@metapatrol/ng';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { repoPath } from '../../core/format';
import { lastPoll, repoHealth } from '../../core/repos';
import { autoRefresh } from '../../core/refresh';

@Component({
  selector: 'app-repositories',
  imports: [RouterLink, MpLoadState, MpPageHeader, MpTime],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Repositories" [note]="repos.body()?.length?.toString()" />
    <mp-load-state
      [error]="repos.error()"
      [loading]="repos.isLoading() && !repos.hasValue()"
      what="repositories"
    />
    @if (repos.hasValue()) {
      <section class="mp-panel mp-panel-flush">
        <div class="max-w-full overflow-x-auto">
          <table class="mp-table w-full">
            <thead>
              <tr>
                <th scope="col">repository</th>
                <th scope="col">pairs</th>
                <th scope="col">queue</th>
              </tr>
            </thead>
            <tbody>
              @for (r of repos.body() ?? []; track r.key) {
                <tr>
                  <td class="w-full max-w-0 min-w-56">
                    <a
                      [routerLink]="['/repositories', ...repoPath(r.key)]"
                      class="mp-link block truncate font-mono text-sm"
                      >{{ r.github }}</a
                    >
                    <span class="block truncate font-mono text-xs text-fg-subtle"
                      >↔ {{ r.forgejo }}</span
                    >
                  </td>
                  <td class="font-mono text-xs whitespace-nowrap">
                    <span class="block"
                      >issues {{ r.counts.issuesOpen }} open ·
                      {{ r.counts.issuesClosed }} closed</span
                    >
                    @if (r.pullRequests) {
                      <span class="block"
                        >PRs {{ r.counts.pullsOpen }} open · {{ r.counts.pullsMerged }} merged ·
                        {{ r.counts.pullsClosed }} closed</span
                      >
                    } @else {
                      <span class="block text-fg-subtle">PRs off</span>
                    }
                  </td>
                  <td class="text-xs whitespace-nowrap">
                    <span class="mp-status" [attr.data-status]="health(r).status">{{
                      health(r).text
                    }}</span>
                    <span class="block text-fg-subtle">
                      @if (lastPoll(r); as at) {
                        polled <mp-time [value]="at" />
                      } @else {
                        not polled yet
                      }
                    </span>
                  </td>
                </tr>
              } @empty {
                <tr>
                  <td colspan="3" class="text-sm text-fg-subtle">No repositories configured.</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      </section>
    }
  `,
})
export class Repositories {
  protected readonly repos = apiResource(listRepositories, () => ({}));
  protected readonly repoPath = repoPath;
  protected readonly health = repoHealth;
  protected readonly lastPoll = lastPoll;

  constructor() {
    autoRefresh(this.repos);
  }
}
