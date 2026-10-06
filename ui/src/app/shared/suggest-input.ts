import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  linkedSignal,
  output,
  resource,
  signal,
} from '@angular/core';
import { MpIcon } from '@metapatrol/ng';

/** How many suggestions show at once. */
export const MAX_SUGGESTIONS = 8;

/** One suggestion: what is chosen, what people read, and a muted hint beside it. */
export interface Suggestion<T = unknown> {
  value: string;
  label: string;
  hint?: string;
  data?: T;
}

let next = 0;

/**
 * A filter field that suggests as the user types, after engram's suggest
 * input: an ARIA combobox whose list opens right under the input, at least as
 * wide as it. The caller searches (search(q) answers with the suggestions,
 * usually from the API), so it scales to any number of them. Only a
 * suggestion, chosen with the mouse or the keyboard, or one typed out in
 * full, is picked; an emptied field, or the clear button, clears; anything
 * else goes back to the current value.
 */
@Component({
  selector: 'app-suggest-input',
  imports: [MpIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'relative block' },
  template: `
    <label class="sr-only" [for]="inputId">{{ label() }}</label>
    <input
      [id]="inputId"
      class="mp-input w-full font-mono"
      [class.pr-8]="!!value()"
      [class.pr-24]="!!value() && !!badge()"
      [placeholder]="placeholder()"
      [value]="text()"
      role="combobox"
      aria-autocomplete="list"
      [attr.aria-expanded]="showList()"
      [attr.aria-controls]="listId"
      [attr.aria-activedescendant]="active() >= 0 ? listId + '-' + active() : null"
      autocomplete="off"
      spellcheck="false"
      (input)="type($any($event.target).value)"
      (focus)="focus()"
      (blur)="leave()"
      (keydown)="key($event)"
    />
    @if (value()) {
      @if (badge() && text() === value()) {
        <span
          class="pointer-events-none absolute top-1/2 right-9 -translate-y-1/2 font-mono text-xs text-fg-subtle"
          >{{ badge() }}</span
        >
      }
      <button
        type="button"
        class="absolute top-1/2 right-1.5 grid size-6 -translate-y-1/2 place-items-center rounded-full text-fg-muted hover:bg-surface-2 hover:text-fg"
        [attr.aria-label]="clearLabel()"
        (mousedown)="$event.preventDefault()"
        (click)="clear()"
      >
        <mp-icon name="x" class="size-3.5" />
      </button>
    }
    @if (showList()) {
      <ul
        [id]="listId"
        role="listbox"
        [attr.aria-label]="label()"
        class="absolute top-full left-0 z-40 mt-1 max-h-72 w-max max-w-[min(36rem,calc(100vw-2rem))] min-w-full overflow-y-auto rounded-md border border-line bg-surface py-1 shadow-lg"
      >
        @for (s of suggestions(); track s.value + (s.hint ?? ''); let i = $index) {
          <li
            [id]="listId + '-' + i"
            role="option"
            [attr.aria-selected]="i === active()"
            class="flex cursor-pointer items-baseline justify-between gap-4 px-3 py-1.5 text-sm text-fg"
            [class.bg-surface-2]="i === active()"
            (mousedown)="$event.preventDefault(); pick(s)"
            (mouseenter)="active.set(i)"
          >
            <span class="min-w-0 truncate font-mono">{{ s.label }}</span>
            @if (s.hint) {
              <span class="shrink-0 font-mono text-xs text-fg-subtle">{{ s.hint }}</span>
            }
          </li>
        }
      </ul>
    }
  `,
})
export class SuggestInput {
  /** The current value as the field shows it; empty for none. */
  readonly value = input<string | undefined>('');
  /** A muted word beside the current value, e.g. its side. */
  readonly badge = input<string | undefined>('');
  readonly label = input.required<string>();
  readonly placeholder = input('');
  readonly clearLabel = input('Clear');
  /** Searches for what is typed; '' for the first suggestions. */
  readonly search = input.required<(q: string) => Promise<Suggestion[]>>();

  readonly picked = output<Suggestion>();
  readonly cleared = output<void>();

  protected readonly inputId = `suggest-input-${next++}`;
  protected readonly listId = `${this.inputId}-suggestions`;
  protected readonly text = linkedSignal(() => this.value() ?? '');
  protected readonly open = signal(false);
  protected readonly active = signal(-1);

  /** The text asked for, a moment after the last keystroke. */
  private readonly asked = signal('');
  private debounce?: ReturnType<typeof setTimeout>;

  private readonly found = resource({
    params: () => (this.open() ? { q: this.asked(), search: this.search() } : undefined),
    loader: ({ params }) => params.search(params.q),
  });

  /** While a new answer loads, the last one stays. */
  protected readonly suggestions = linkedSignal<Suggestion[] | undefined, Suggestion[]>({
    source: () => (this.found.hasValue() ? this.found.value() : undefined),
    computation: (fresh, prev) => fresh ?? prev?.value ?? [],
  });

  protected readonly showList = computed(() => {
    const s = this.suggestions();
    return this.open() && s.length > 0 && !(s.length === 1 && s[0].label === this.text());
  });

  protected type(v: string): void {
    this.text.set(v);
    clearTimeout(this.debounce);
    this.debounce = setTimeout(() => this.asked.set(v.trim()), 150);
    this.open.set(true);
    this.active.set(-1);
  }

  protected focus(): void {
    // The field shows the current value: suggest from the start, not from it.
    this.asked.set(this.text().trim() === (this.value() ?? '') ? '' : this.text().trim());
    this.open.set(true);
  }

  protected pick(s: Suggestion): void {
    this.text.set(s.label);
    this.open.set(false);
    this.active.set(-1);
    this.picked.emit(s);
  }

  protected clear(): void {
    this.text.set('');
    this.asked.set('');
    if (this.value()) {
      this.cleared.emit();
    }
  }

  protected leave(): void {
    this.open.set(false);
    this.active.set(-1);
    this.settle();
  }

  /** Emptied: cleared. Typed out in full: picked. Anything else: back to the current value. */
  private settle(): void {
    const t = this.text().trim();
    if (!t) {
      if (this.value()) {
        this.cleared.emit();
      }
      return;
    }
    if (t === (this.value() ?? '')) {
      return;
    }
    const hits = this.suggestions().filter((s) => s.label.toLowerCase() === t.toLowerCase());
    if (hits.length === 1) {
      this.pick(hits[0]);
    } else {
      this.text.set(this.value() ?? '');
    }
  }

  protected key(e: KeyboardEvent): void {
    const n = this.suggestions().length;
    switch (e.key) {
      case 'ArrowDown':
        if (n) {
          e.preventDefault();
          this.open.set(true);
          this.active.update((i) => (i + 1) % n);
        }
        break;
      case 'ArrowUp':
        if (n) {
          e.preventDefault();
          this.open.set(true);
          this.active.update((i) => (i <= 0 ? n - 1 : i - 1));
        }
        break;
      case 'Enter':
        e.preventDefault();
        if (this.showList() && this.active() >= 0) {
          this.pick(this.suggestions()[this.active()]);
        } else {
          this.open.set(false);
          this.settle();
        }
        break;
      case 'Escape':
        if (this.showList()) {
          e.preventDefault();
          this.open.set(false);
        }
        break;
    }
  }
}
