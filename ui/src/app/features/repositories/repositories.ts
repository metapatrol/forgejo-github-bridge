import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpLoadState, MpPageHeader, MpTime } from '@metapatrol/ng';
import { listRepositories } from '../../api/fn/repositories/list-repositories';
import { apiResource } from '../../core/api';
import { repoPath } from '../../core/format';
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
      <section class="mp-panel mp-panel-flush overflow-x-auto">
        <table class="mp-table">
          <thead>
            <tr>
              <th scope="col">GitHub</th>
              <th scope="col">Forgejo</th>
              <th scope="col">issues</th>
              <th scope="col">pull requests</th>
              <th scope="col">queue</th>
              <th scope="col">last poll</th>
            </tr>
          </thead>
          <tbody>
            @for (r of repos.body() ?? []; track r.key) {
              <tr>
                <td class="font-mono text-sm whitespace-nowrap">
                  <a [routerLink]="['/repositories', ...repoPath(r.key)]" class="mp-link">{{
                    r.github
                  }}</a>
                </td>
                <td class="font-mono text-sm whitespace-nowrap text-fg-muted">{{ r.forgejo }}</td>
                <td class="font-mono text-xs whitespace-nowrap">
                  {{ r.counts.issuesOpen }} open · {{ r.counts.issuesClosed }} closed
                </td>
                <td class="font-mono text-xs whitespace-nowrap">
                  @if (r.pullRequests) {
                    {{ r.counts.pullsOpen }} open · {{ r.counts.pullsMerged }} merged ·
                    {{ r.counts.pullsClosed }} closed
                  } @else {
                    <span class="text-fg-subtle">off</span>
                  }
                </td>
                <td class="text-xs whitespace-nowrap">
                  @if (r.queue.failing > 0) {
                    <span class="mp-status" data-status="error">{{ r.queue.failing }} failing</span>
                  } @else if (r.queue.depth > 0) {
                    <span class="mp-status" data-status="active">{{ r.queue.depth }} queued</span>
                  } @else {
                    <span class="mp-status" data-status="success">in step</span>
                  }
                </td>
                <td class="text-xs whitespace-nowrap text-fg-muted">
                  @if (lastPoll(r.cursors); as at) {
                    <mp-time [value]="at" />
                  } @else {
                    —
                  }
                </td>
              </tr>
            } @empty {
              <tr>
                <td colspan="6" class="text-sm text-fg-subtle">No repositories configured.</td>
              </tr>
            }
          </tbody>
        </table>
      </section>
    }
  `,
})
export class Repositories {
  protected readonly repos = apiResource(listRepositories, () => ({}));
  protected readonly repoPath = repoPath;

  constructor() {
    autoRefresh(this.repos);
  }

  /** When any poll cursor of the repository last moved. */
  protected lastPoll(cursors: { updatedAt: string }[]): string | undefined {
    return cursors
      .map((c) => c.updatedAt)
      .sort()
      .at(-1);
  }
}
