import {
  booleanAttribute,
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  output,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpSort, MpTime, Sort, timestamp } from '@metapatrol/ng';
import { ActivityEntry } from '../api/models/activity-entry';
import { activitySentence, pairNoun } from '../core/activity';
import { pairLink } from '../core/kinds';
import { repositoryLink } from '../core/links';
import { SideLabel } from './badges';
import { NearEnd } from './near-end';
import { QUIET_LINK } from './tile';

/** The activity's order unless ?sort= says otherwise: the newest first. */
export const ACTIVITY_SORT: Sort = { key: 'at', desc: true };

/**
 * What the bridge wrote, one row each: when, what as a sentence, the pair it
 * belongs to, where it happened. Compact for panels: when, what, and where.
 */
@Component({
  selector: 'app-activity-table',
  imports: [RouterLink, MpSort, MpTime, NearEnd, SideLabel],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block max-w-full overflow-x-auto' },
  template: `
    <table class="mp-table w-full">
      <thead>
        <tr>
          @for (h of headers(); track h.key) {
            <th scope="col" [class]="h.class">
              @if (sortable() && h.sort) {
                <mp-sort [key]="h.sort" [sort]="sort()" [fallback]="fallback">{{
                  h.label
                }}</mp-sort>
              } @else {
                {{ h.label }}
              }
            </th>
          }
        </tr>
      </thead>
      <tbody>
        @for (e of entries(); track e.id) {
          <tr>
            <td class="text-xs whitespace-nowrap text-fg-muted" [title]="absolute(e.at)">
              <mp-time [value]="e.at" />
            </td>
            <td class="w-full max-w-0 min-w-64">
              <span class="block truncate">
                {{ sentence(e) }}
                @if (e.pairId && e.pairKind) {
                  <span class="text-fg-subtle">&nbsp;·&nbsp;</span>
                  <a [routerLink]="pair(e)" class="mp-link text-xs">{{ noun(e) }}</a>
                }
              </span>
            </td>
            @if (!compact()) {
              <td class="font-mono text-xs whitespace-nowrap text-fg-muted">{{ e.kind }}</td>
              <td><app-side [side]="e.side" /></td>
            }
            @if (showRepository()) {
              <td class="font-mono text-xs whitespace-nowrap">
                <a [routerLink]="repo(e.repository)" [class]="quiet">{{ e.repository }}</a>
              </td>
            }
            <td class="text-right">
              @if (e.url) {
                <a
                  [href]="e.url"
                  class="mp-link text-xs"
                  target="_blank"
                  rel="noopener noreferrer"
                  [attr.aria-label]="'Open on ' + (e.side === 'github' ? 'GitHub' : 'Forgejo')"
                  >↗</a
                >
              }
            </td>
          </tr>
        } @empty {
          <tr>
            <td [attr.colspan]="headers().length" class="text-sm text-fg-subtle">{{ empty() }}</td>
          </tr>
        }
        @if (more()) {
          <tr (appNearEnd)="nearEnd.emit()">
            <td [attr.colspan]="headers().length" class="text-center text-xs text-fg-subtle">
              loading more…
            </td>
          </tr>
        }
      </tbody>
    </table>
  `,
})
export class ActivityTable {
  readonly entries = input.required<readonly ActivityEntry[]>();
  /** Off where the activity is of one repository already. */
  readonly showRepository = input(true);
  /** For panels: when, what and where, without kind and side. */
  readonly compact = input(false, { transform: booleanAttribute });
  readonly empty = input('Nothing written yet.');
  /** The list's ?sort=, when its headers sort it. */
  readonly sort = input<string | undefined>();
  readonly sortable = input(false, { transform: booleanAttribute });
  /** Whether more rows can be loaded; the last row asks for them when it comes into view. */
  readonly more = input(false);
  readonly nearEnd = output<void>();

  protected readonly fallback = ACTIVITY_SORT;
  protected readonly quiet = QUIET_LINK;
  protected readonly sentence = activitySentence;
  protected readonly noun = pairNoun;
  protected readonly absolute = timestamp;

  protected readonly headers = computed<
    { key: string; label: string; sort?: string; class: string }[]
  >(() => [
    { key: 'at', label: 'when', sort: 'at', class: '' },
    { key: 'what', label: 'what', sort: 'action', class: '' },
    ...(this.compact()
      ? []
      : [
          { key: 'kind', label: 'kind', sort: 'kind', class: '' },
          { key: 'side', label: 'side', sort: 'side', class: '' },
        ]),
    ...(this.showRepository()
      ? [{ key: 'repository', label: 'repository', sort: 'repository', class: '' }]
      : []),
    { key: 'url', label: '', class: 'w-8' },
  ]);

  protected pair(e: ActivityEntry): (string | number)[] {
    return e.pairKind && e.pairId ? pairLink(e.pairKind, e.pairId) : [];
  }

  protected repo(key: string): (string | number)[] {
    return repositoryLink(key).commands;
  }
}
