import { booleanAttribute, ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpSort, MpTime, Sort } from '@metapatrol/ng';
import { NearEnd } from './near-end';
import { Pair } from '../api/models/pair';
import { PairKind } from '../api/models/pair-kind';
import { pairLink } from '../core/kinds';
import { repositoryLink } from '../core/links';
import { QUIET_LINK } from './tile';
import { Author } from './author';
import { PullFlags, SideLabel, StateBadge } from './badges';

/** The order of a list of pairs unless ?sort= says otherwise: the newest first. */
export const PAIRS_SORT: Sort = { key: 'created', desc: true };

/** The columns a list of pairs can be sorted by. */
export const PAIRS_SORTS = [
  'created',
  'updated',
  'title',
  'repository',
  'author',
  'github',
  'forgejo',
  'state',
  'comments',
] as const;

/**
 * Issues or pull requests as a table. The two share the title, the numbers on
 * both sides, the state and the time; issues add where they were opened,
 * pull requests their flags and contrib branch.
 */
@Component({
  selector: 'app-pairs-table',
  imports: [RouterLink, MpSort, MpTime, Author, NearEnd, SideLabel, StateBadge, PullFlags],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block max-w-full overflow-x-auto' },
  template: `
    <table class="mp-table w-full">
      <thead>
        <tr>
          <th scope="col">
            @if (sortable()) {
              <mp-sort key="title" [sort]="sort()" [fallback]="fallback">title</mp-sort>
            } @else {
              title
            }
          </th>
          <th scope="col" class="text-right">
            @if (sortable()) {
              <mp-sort key="github" [sort]="sort()" [fallback]="fallback">GitHub</mp-sort>
            } @else {
              GitHub
            }
          </th>
          <th scope="col" class="text-right">
            @if (sortable()) {
              <mp-sort key="forgejo" [sort]="sort()" [fallback]="fallback">Forgejo</mp-sort>
            } @else {
              Forgejo
            }
          </th>
          @if (kind() === 'issue') {
            <th scope="col">origin</th>
          }
          <th scope="col">
            @if (sortable()) {
              <mp-sort key="state" [sort]="sort()" [fallback]="fallback">state</mp-sort>
            } @else {
              state
            }
          </th>
          @if (kind() === 'pull') {
            <th scope="col" class="hidden xl:table-cell">flags</th>
            <th scope="col" class="hidden xl:table-cell">branch</th>
          }
          <th scope="col" class="hidden text-right xl:table-cell">
            @if (sortable()) {
              <mp-sort key="comments" [sort]="sort()" [fallback]="fallback">comments</mp-sort>
            } @else {
              comments
            }
          </th>
          <th scope="col" class="hidden xl:table-cell">
            @if (sortable()) {
              <mp-sort key="created" [sort]="sort()" [fallback]="fallback">opened</mp-sort>
            } @else {
              opened
            }
          </th>
          <th scope="col">
            @if (sortable()) {
              <mp-sort key="updated" [sort]="sort()" [fallback]="fallback">updated</mp-sort>
            } @else {
              updated
            }
          </th>
        </tr>
      </thead>
      <tbody>
        @for (p of pairs(); track p.id) {
          <tr>
            <td class="w-full max-w-0 min-w-56">
              <a [routerLink]="link(p)" class="mp-link">{{ p.title || '(no title yet)' }}</a>
              <span class="block truncate font-mono text-xs text-fg-subtle">
                @if (showRepository()) {
                  <a [routerLink]="repoLink(p.repository)" [class]="quiet">{{ p.repository }}</a>
                }
                @if (showRepository() && p.author && showAuthor()) {
                  ·
                }
                @if (p.author && showAuthor()) {
                  by <app-author [login]="p.author" [side]="p.origin" />
                }
              </span>
            </td>
            <td class="text-right font-mono text-xs whitespace-nowrap">
              <a [href]="p.githubUrl" class="mp-link" target="_blank" rel="noopener noreferrer"
                >#{{ p.githubNumber }}</a
              >
            </td>
            <td class="text-right font-mono text-xs whitespace-nowrap">
              <a [href]="p.forgejoUrl" class="mp-link" target="_blank" rel="noopener noreferrer"
                >#{{ p.forgejoNumber }}</a
              >
            </td>
            @if (kind() === 'issue') {
              <td><app-side [side]="p.origin" /></td>
            }
            <td>
              <span class="inline-flex flex-wrap items-center gap-1">
                <app-state [state]="p.state" />
                @if (kind() === 'pull') {
                  <app-pull-flags class="xl:hidden" [pair]="p" />
                }
                @if (p.queued; as q) {
                  @if (q.lastError) {
                    <a routerLink="/queue" class="no-underline" [title]="q.lastError">
                      <span class="mp-status text-xs" data-status="error">failing</span>
                    </a>
                  } @else {
                    <span class="mp-status text-xs" data-status="active">queued</span>
                  }
                }
              </span>
            </td>
            @if (kind() === 'pull') {
              <td class="hidden xl:table-cell"><app-pull-flags [pair]="p" /></td>
              <td class="hidden font-mono text-xs whitespace-nowrap xl:table-cell">
                @if (p.pull; as pr) {
                  @if (pr.contribBranchUrl) {
                    <a
                      [href]="pr.contribBranchUrl"
                      class="mp-link"
                      target="_blank"
                      rel="noopener noreferrer"
                      >{{ pr.contribBranch }}</a
                    >
                  } @else {
                    {{ pr.contribBranch }}
                  }
                } @else {
                  —
                }
              </td>
            }
            <td class="mp-num hidden xl:table-cell">{{ p.comments }}</td>
            <td class="hidden text-xs whitespace-nowrap text-fg-muted xl:table-cell">
              <mp-time [value]="p.createdAt" />
            </td>
            <td class="text-xs whitespace-nowrap text-fg-muted">
              <mp-time [value]="p.updatedAt" />
            </td>
          </tr>
        } @empty {
          <tr>
            <td [attr.colspan]="kind() === 'pull' ? 9 : 8" class="text-sm text-fg-subtle">
              {{ empty() }}
            </td>
          </tr>
        }
        @if (more()) {
          <tr (appNearEnd)="nearEnd.emit()">
            <td
              [attr.colspan]="kind() === 'pull' ? 9 : 8"
              class="text-center text-xs text-fg-subtle"
            >
              loading more…
            </td>
          </tr>
        }
      </tbody>
    </table>
  `,
})
export class PairsTable {
  readonly pairs = input.required<readonly Pair[]>();
  readonly kind = input.required<PairKind>();
  /** Off where the list is of one repository already. */
  readonly showRepository = input(true);
  readonly empty = input('Nothing here.');
  /** Off where the list is of one contributor already. */
  readonly showAuthor = input(true);
  /** The list's ?sort=, when its headers sort it. */
  readonly sort = input<string | undefined>();
  readonly sortable = input(false, { transform: booleanAttribute });
  /** Whether more rows can be loaded; the last row asks for them when it comes into view. */
  readonly more = input(false);
  readonly nearEnd = output<void>();
  protected readonly fallback = PAIRS_SORT;

  protected readonly quiet = QUIET_LINK;

  protected repoLink(key: string): (string | number)[] {
    return repositoryLink(key).commands;
  }

  protected link(p: Pair): (string | number)[] {
    return pairLink(p.kind, p.id);
  }
}
