import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

/**
 * How far a value has come towards a limit, as a thin bar: calm below
 * warnAt, a warning from there, and an error at the limit.
 */
@Component({
  selector: 'app-meter',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block' },
  template: `
    <div
      class="h-1.5 overflow-hidden rounded-full bg-surface-2"
      role="meter"
      [attr.aria-label]="label()"
      [attr.aria-valuenow]="value()"
      aria-valuemin="0"
      [attr.aria-valuemax]="max()"
    >
      <span class="block h-full rounded-full" [class]="tone()" [style.width.%]="percent()"></span>
    </div>
  `,
})
export class Meter {
  readonly value = input.required<number>();
  readonly max = input.required<number>();
  readonly label = input('');
  /** The share of max from which it shows as a warning. */
  readonly warnAt = input(0.75);

  protected readonly ratio = computed(() => (this.max() > 0 ? this.value() / this.max() : 0));
  protected readonly percent = computed(() => Math.min(100, Math.max(0, this.ratio() * 100)));
  protected readonly tone = computed(() => {
    const r = this.ratio();
    return r >= 1 ? 'bg-error' : r >= this.warnAt() ? 'bg-warning' : 'bg-signal';
  });
}
