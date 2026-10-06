import {
  booleanAttribute,
  ChangeDetectionStrategy,
  Component,
  inject,
  input,
  output,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpClock, MpSort, MpTime, Sort } from '@metapatrol/ng';
import { NearEnd } from './near-end';
import { QueueEntry } from '../api/models/queue-entry';
import { until } from '../core/format';
import { KINDS, pairLink } from '../core/kinds';
import { repositoryLink } from '../core/links';
import { QUIET_LINK } from './tile';
import { PairKind } from '../api/models/pair-kind';
import { SideLabel } from './badges';

/** The queue's order unless ?sort= says otherwise: failing first, then by when due. */
export const QUEUE_SORT: Sort = { key: 'due', desc: false };

/** The columns the queue can be sorted by. */
export const QUEUE_SORTS = [
  'due',
  'next',
  'marked',
  'attempts',
  'repository',
  'number',
  'error',
] as const;

/** Queued work: failing entries with their last error, the rest with when they are due. */
@Component({
  selector: 'app-queue-table',
  imports: [RouterLink, MpSort, MpTime, NearEnd, SideLabel],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block max-w-full overflow-x-auto' },
  template: `
    <table class="mp-table w-full min-w-[52rem] table-fixed">
      <thead>
        <tr>
          <th scope="col" class="w-56">
            @if (sortable()) {
              <mp-sort key="number" [sort]="sort()" [fallback]="fallback">item</mp-sort>
            } @else {
              item
            }
          </th>
          @if (showRepository()) {
            <th scope="col" class="w-52">
              @if (sortable()) {
                <mp-sort key="repository" [sort]="sort()" [fallback]="fallback">repository</mp-sort>
              } @else {
                repository
              }
            </th>
          }
          <th scope="col" class="w-24">
            @if (sortable()) {
              <mp-sort key="attempts" [sort]="sort()" [fallback]="fallback">attempts</mp-sort>
            } @else {
              attempts
            }
          </th>
          <th scope="col" class="w-24">
            @if (sortable()) {
              <mp-sort key="next" [sort]="sort()" [fallback]="fallback">next try</mp-sort>
            } @else {
              next try
            }
          </th>
          <th scope="col" class="w-24">
            @if (sortable()) {
              <mp-sort key="marked" [sort]="sort()" [fallback]="fallback">marked</mp-sort>
            } @else {
              marked
            }
          </th>
          <th scope="col" class="">
            @if (sortable()) {
              <mp-sort key="error" [sort]="sort()" [fallback]="fallback">last error</mp-sort>
            } @else {
              last error
            }
          </th>
        </tr>
      </thead>
      <tbody>
        @for (e of entries(); track e.repository + e.side + e.number) {
          <tr>
            <td class="whitespace-nowrap">
              <span class="flex items-baseline gap-2">
                <app-side [side]="e.side" />
                <a
                  [href]="e.url"
                  class="mp-link font-mono"
                  target="_blank"
                  rel="noopener noreferrer"
                  >#{{ e.number }}</a
                >
                @if (e.pairId && e.kind) {
                  <a [routerLink]="link(e.kind, e.pairId)" class="mp-link text-xs"
                    >{{ kindName(e.kind) }} →</a
                  >
                } @else if (e.pairId) {
                  <a [routerLink]="['/pairs', e.pairId]" class="mp-link text-xs">pair →</a>
                }
              </span>
            </td>
            @if (showRepository()) {
              <td class="truncate font-mono text-xs" [title]="e.repository">
                <a [routerLink]="repoLink(e.repository)" [class]="quiet">{{ e.repository }}</a>
              </td>
            }
            <td class="font-mono tabular-nums" [class.text-error]="e.attempts > 0">
              {{ e.attempts }}
            </td>
            <td class="text-xs whitespace-nowrap" [title]="e.notBefore">
              {{ due(e.notBefore) }}
            </td>
            <td class="text-xs whitespace-nowrap text-fg-muted">
              <mp-time [value]="e.markedAt" />
            </td>
            <td class="min-w-0">
              @if (e.lastError) {
                <details class="group min-w-0">
                  <summary class="block cursor-pointer truncate font-mono text-xs text-error">
                    <span class="group-open:hidden" title="Show the whole error">{{
                      firstLine(e.lastError)
                    }}</span>
                    <span class="hidden text-fg-muted group-open:inline">hide</span>
                  </summary>
                  <pre
                    class="m-0 cursor-text font-mono text-xs break-words whitespace-pre-wrap text-error"
                    >{{ e.lastError }}</pre>
                </details>
              } @else {
                <span class="text-xs text-fg-subtle">—</span>
              }
            </td>
          </tr>
        } @empty {
          <tr>
            <td [attr.colspan]="showRepository() ? 6 : 5" class="text-sm text-fg-subtle">
              {{ empty() }}
            </td>
          </tr>
        }
        @if (more()) {
          <tr (appNearEnd)="nearEnd.emit()">
            <td
              [attr.colspan]="showRepository() ? 6 : 5"
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
export class QueueTable {
  readonly entries = input.required<readonly QueueEntry[]>();
  readonly showRepository = input(true);
  readonly empty = input('Nothing is queued.');
  /** The list's ?sort=, when its headers sort it. */
  readonly sort = input<string | undefined>();
  readonly sortable = input(false, { transform: booleanAttribute });
  /** Whether more rows can be loaded; the last row asks for them when it comes into view. */
  readonly more = input(false);
  readonly nearEnd = output<void>();
  protected readonly fallback = QUEUE_SORT;
  private readonly clock = inject(MpClock);

  protected due(at: string): string {
    return until(at, this.clock.now());
  }

  protected readonly quiet = QUIET_LINK;

  protected repoLink(key: string): (string | number)[] {
    return repositoryLink(key).commands;
  }

  protected link(kind: PairKind, id: number): (string | number)[] {
    return pairLink(kind, id);
  }

  protected kindName(kind: PairKind): string {
    return KINDS[kind].one;
  }

  protected firstLine(text: string): string {
    return text.split('\n')[0];
  }
}
