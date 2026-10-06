import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

/** "51–100 of 230" with previous and next, for offset-paged lists. */
@Component({
  selector: 'app-pager',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'flex items-center gap-3 text-sm text-fg-muted' },
  template: `
    <span class="font-mono text-xs tabular-nums">{{ range() }}</span>
    <button
      type="button"
      class="mp-btn-ghost"
      [disabled]="offset() === 0"
      (click)="moved.emit(previousOffset())"
      aria-label="Previous page"
    >
      ←
    </button>
    <button
      type="button"
      class="mp-btn-ghost"
      [disabled]="offset() + limit() >= total()"
      (click)="moved.emit(offset() + limit())"
      aria-label="Next page"
    >
      →
    </button>
  `,
})
export class Pager {
  readonly offset = input(0);
  readonly limit = input(50);
  /** Rows on this page. */
  readonly shown = input(0);
  /** Rows on all pages. */
  readonly total = input(0);
  /** The offset to go to. */
  readonly moved = output<number>();

  protected readonly previousOffset = computed(() => Math.max(0, this.offset() - this.limit()));
  protected readonly range = computed(() => {
    if (!this.shown()) {
      return `0 of ${this.total()}`;
    }
    const from = this.offset() + 1;
    return `${from}–${from + this.shown() - 1} of ${this.total()}`;
  });
}
