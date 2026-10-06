import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { MpListSearch, MpLoadState, MpPageHeader, MpSort, MpTime, Sort } from '@metapatrol/ng';
import { ListAuthors$Params, listAuthors } from '../../api/fn/pairs/list-authors';
import { pagedList, sortParam } from '../../core/paged';
import { NearEnd } from '../../shared/near-end';
import { liveReload } from '../../core/events';
import { Author } from '../../shared/author';
import { SideLabel } from '../../shared/badges';

/** The contributors' order unless ?sort= says otherwise: the most active first. */
const CONTRIBUTORS_SORT: Sort = { key: 'pairs', desc: true };
const CONTRIBUTORS_SORTS = ['pairs', 'issues', 'pulls', 'last', 'login'];

/** Everyone who opened a pair, the most active first. */
@Component({
  selector: 'app-contributors',
  imports: [MpListSearch, MpLoadState, MpPageHeader, MpSort, MpTime, Author, NearEnd, SideLabel],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Contributors" [note]="authors.total().toString()" />
    <p class="mb-4 max-w-3xl text-sm text-fg-muted">
      Who opened the issues and pull requests the bridge holds: GitHub logins for what came from
      GitHub, Forgejo logins for issues published from Forgejo. The bridge does not record who wrote
      the comments.
    </p>
    <section class="mp-panel mp-panel-flush">
      <header class="mp-panel-head">
        <mp-list-search
          placeholder="Login"
          [query]="q()"
          [shown]="authors.items().length"
          [total]="authors.total()"
        />
      </header>
      <mp-load-state
        [error]="authors.error()"
        [loading]="authors.loading() && !authors.loaded()"
        what="contributors"
      />
      @if (authors.loaded()) {
        <div class="max-w-full overflow-x-auto">
          <table class="mp-table w-full">
            <thead>
              <tr>
                <th scope="col" class="w-full">
                  <mp-sort key="login" [sort]="sort()" [fallback]="fallback">login</mp-sort>
                </th>
                <th scope="col">side</th>
                <th scope="col" class="text-right">
                  <mp-sort key="pairs" [sort]="sort()" [fallback]="fallback">pairs</mp-sort>
                </th>
                <th scope="col" class="text-right">
                  <mp-sort key="issues" [sort]="sort()" [fallback]="fallback">issues</mp-sort>
                </th>
                <th scope="col" class="text-right">
                  <mp-sort key="pulls" [sort]="sort()" [fallback]="fallback">pull requests</mp-sort>
                </th>
                <th scope="col">
                  <mp-sort key="last" [sort]="sort()" [fallback]="fallback">last</mp-sort>
                </th>
              </tr>
            </thead>
            <tbody>
              @for (a of authors.items(); track a.side + a.login) {
                <tr>
                  <td><app-author [login]="a.login" [side]="a.side" /></td>
                  <td><app-side [side]="a.side" /></td>
                  <td class="mp-num">{{ a.issues + a.pulls }}</td>
                  <td class="mp-num">{{ a.issues }}</td>
                  <td class="mp-num">{{ a.side === 'github' ? a.pulls : '—' }}</td>
                  <td class="text-xs whitespace-nowrap text-fg-muted">
                    <mp-time [value]="a.lastAt" />
                  </td>
                </tr>
              } @empty {
                <tr>
                  <td colspan="6" class="text-sm text-fg-subtle">
                    {{ q() ? 'No contributor matches.' : 'Nobody yet.' }}
                  </td>
                </tr>
              }
              @if (authors.more()) {
                <tr (appNearEnd)="authors.loadMore()">
                  <td colspan="6" class="text-center text-xs text-fg-subtle">loading more…</td>
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
  readonly q = input<string>();
  /** ?sort=: a column, descending with a leading -; the most active first by default. */
  readonly sort = input<string>();
  protected readonly fallback = CONTRIBUTORS_SORT;
  protected readonly authors = pagedList(listAuthors, () => ({
    q: this.q() || undefined,
    sort: sortParam<NonNullable<ListAuthors$Params['sort']>>(this.sort(), CONTRIBUTORS_SORTS),
  }));

  constructor() {
    liveReload(() => ({ view: 'contributors' }), this.authors);
  }
}
