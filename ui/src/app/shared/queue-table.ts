import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpClock, MpTime } from '@metapatrol/ng';
import { QueueEntry } from '../api/models/queue-entry';
import { until } from '../core/format';
import { SideLabel } from './badges';

/** Queued work: failing entries with their last error, the rest with when they are due. */
@Component({
  selector: 'app-queue-table',
  imports: [RouterLink, MpTime, SideLabel],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block max-w-full overflow-x-auto' },
  template: `
    <table class="mp-table w-full min-w-[52rem] table-fixed">
      <thead>
        <tr>
          <th scope="col" class="w-44">item</th>
          @if (showRepository()) {
            <th scope="col" class="w-52">repository</th>
          }
          <th scope="col" class="w-20">attempts</th>
          <th scope="col" class="w-24">next try</th>
          <th scope="col" class="w-24">marked</th>
          <th scope="col">last error</th>
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
                @if (e.pairId) {
                  <a [routerLink]="['/pairs', e.pairId]" class="mp-link text-xs">pair →</a>
                }
              </span>
            </td>
            @if (showRepository()) {
              <td class="truncate font-mono text-xs" [title]="e.repository">{{ e.repository }}</td>
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
      </tbody>
    </table>
  `,
})
export class QueueTable {
  readonly entries = input.required<readonly QueueEntry[]>();
  readonly showRepository = input(true);
  readonly empty = input('Nothing is queued.');
  private readonly clock = inject(MpClock);

  protected due(at: string): string {
    return until(at, this.clock.now());
  }

  protected firstLine(text: string): string {
    return text.split('\n')[0];
  }
}
