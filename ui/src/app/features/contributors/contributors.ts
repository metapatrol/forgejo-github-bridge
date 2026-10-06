import { ChangeDetectionStrategy, Component } from '@angular/core';
import { MpLoadState, MpPageHeader, MpTime } from '@metapatrol/ng';
import { listAuthors } from '../../api/fn/pairs/list-authors';
import { apiResource } from '../../core/api';
import { liveReload } from '../../core/events';
import { Author } from '../../shared/author';
import { SideLabel } from '../../shared/badges';

/** Everyone who opened a pair, the most active first. */
@Component({
  selector: 'app-contributors',
  imports: [MpLoadState, MpPageHeader, MpTime, Author, SideLabel],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Contributors" [note]="authors.body()?.length?.toString()" />
    <p class="mb-4 max-w-3xl text-sm text-fg-muted">
      Who opened the issues and pull requests the bridge holds: GitHub logins for what came from
      GitHub, Forgejo logins for issues published from Forgejo. The bridge does not record who wrote
      the comments.
    </p>
    <section class="mp-panel mp-panel-flush">
      <mp-load-state
        [error]="authors.error()"
        [loading]="authors.isLoading() && !authors.hasValue()"
        what="contributors"
      />
      @if (authors.hasValue()) {
        <div class="max-w-full overflow-x-auto">
          <table class="mp-table w-full">
            <thead>
              <tr>
                <th scope="col" class="w-full">login</th>
                <th scope="col">side</th>
                <th scope="col" class="mp-num">issues</th>
                <th scope="col" class="mp-num">pull requests</th>
                <th scope="col">last</th>
              </tr>
            </thead>
            <tbody>
              @for (a of authors.body() ?? []; track a.side + a.login) {
                <tr>
                  <td><app-author [login]="a.login" [side]="a.side" /></td>
                  <td><app-side [side]="a.side" /></td>
                  <td class="mp-num">{{ a.issues }}</td>
                  <td class="mp-num">{{ a.side === 'github' ? a.pulls : '—' }}</td>
                  <td class="text-xs whitespace-nowrap text-fg-muted">
                    <mp-time [value]="a.lastAt" />
                  </td>
                </tr>
              } @empty {
                <tr>
                  <td colspan="5" class="text-sm text-fg-subtle">Nobody yet.</td>
                </tr>
              }
            </tbody>
          </table>
        </div>
      }
    </section>
  `,
})
export class Contributors {
  protected readonly authors = apiResource(listAuthors, () => ({}));

  constructor() {
    liveReload(() => ({ view: 'contributors' }), this.authors);
  }
}
