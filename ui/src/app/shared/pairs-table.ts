import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpTime } from '@metapatrol/ng';
import { Pair } from '../api/models/pair';
import { PairKind } from '../api/models/pair-kind';
import { pairLink } from '../core/kinds';
import { Author } from './author';
import { PullFlags, SideLabel, StateBadge } from './badges';

/**
 * Issues or pull requests as a table. The two share the title, the numbers on
 * both sides, the state and the time; issues add where they were opened,
 * pull requests their flags and contrib branch.
 */
@Component({
  selector: 'app-pairs-table',
  imports: [RouterLink, MpTime, Author, SideLabel, StateBadge, PullFlags],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block max-w-full overflow-x-auto' },
  template: `
    <table class="mp-table w-full">
      <thead>
        <tr>
          <th scope="col">title</th>
          <th scope="col" class="text-right">GitHub</th>
          <th scope="col" class="text-right">Forgejo</th>
          @if (kind() === 'issue') {
            <th scope="col">origin</th>
          }
          <th scope="col">state</th>
          @if (kind() === 'pull') {
            <th scope="col" class="hidden xl:table-cell">flags</th>
            <th scope="col" class="hidden xl:table-cell">branch</th>
          }
          <th scope="col">updated</th>
        </tr>
      </thead>
      <tbody>
        @for (p of pairs(); track p.id) {
          <tr>
            <td class="w-full max-w-0 min-w-56">
              <a [routerLink]="link(p)" class="mp-link">{{ p.title || '(no title yet)' }}</a>
              <span class="block truncate font-mono text-xs text-fg-subtle">
                @if (showRepository()) {
                  {{ p.repository }}
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
            <td class="text-xs whitespace-nowrap text-fg-muted">
              <mp-time [value]="p.updatedAt" />
            </td>
          </tr>
        } @empty {
          <tr>
            <td [attr.colspan]="kind() === 'pull' ? 7 : 6" class="text-sm text-fg-subtle">
              {{ empty() }}
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
  /** Off where the list is of one person already. */
  readonly showAuthor = input(true);

  protected link(p: Pair): (string | number)[] {
    return pairLink(p.kind, p.id);
  }
}
