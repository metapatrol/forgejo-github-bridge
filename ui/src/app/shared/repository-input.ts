import {
  booleanAttribute,
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  linkedSignal,
  output,
  signal,
} from '@angular/core';
import { MpIcon } from '@metapatrol/ng';
import { listRepositories } from '../api/fn/repositories/list-repositories';
import { Repository } from '../api/models/repository';
import { apiResource } from '../core/api';

/** How many suggestions show at once. */
export const MAX_SUGGESTIONS = 8;

let next = 0;

/**
 * A repository filter that suggests repositories as the user types: an ARIA
 * combobox whose list opens right under the input, at least as wide as it,
 * after engram's suggest input. The API searches (GET /repositories?q=), so
 * it scales to any number of them. Only a suggestion, chosen with the mouse
 * or the keyboard, or a GitHub key typed in full, becomes the filter; an
 * emptied field, or the clear button, removes it.
 */
@Component({
  selector: 'app-repository-input',
  imports: [MpIcon],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'relative block' },
  template: `
    <label class="sr-only" [for]="inputId">Repository</label>
    <input
      [id]="inputId"
      class="mp-input w-full pr-8 font-mono"
      placeholder="all repositories"
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
      <button
        type="button"
        class="absolute top-1/2 right-1.5 grid size-6 -translate-y-1/2 place-items-center rounded-full text-fg-muted hover:bg-surface-2 hover:text-fg"
        aria-label="Remove the repository filter"
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
        aria-label="Repositories"
        class="absolute top-full left-0 z-40 mt-1 max-h-72 w-max max-w-[min(36rem,calc(100vw-2rem))] min-w-full overflow-y-auto rounded-md border border-line bg-surface py-1 shadow-lg"
      >
        @for (r of suggestions(); track r.key; let i = $index) {
          <li
            [id]="listId + '-' + i"
            role="option"
            [attr.aria-selected]="i === active()"
            class="flex cursor-pointer items-baseline justify-between gap-4 px-3 py-1.5 text-sm text-fg"
            [class.bg-surface-2]="i === active()"
            (mousedown)="$event.preventDefault(); pick(r)"
            (mouseenter)="active.set(i)"
          >
            <span class="min-w-0 truncate font-mono">{{ r.key }}</span>
            <span class="shrink-0 font-mono text-xs text-fg-subtle">
              {{ r.forgejo }}
            </span>
          </li>
        }
      </ul>
    }
  `,
})
export class RepositoryInput {
  /** The current filter, a GitHub key; empty for all repositories. */
  readonly value = input<string | undefined>('');
  /** Suggest only repositories that import pull requests. */
  readonly pullsOnly = input(false, { transform: booleanAttribute });
  /** The new filter: a key, or '' for all repositories. */
  readonly committed = output<string>();

  protected readonly inputId = `repository-input-${next++}`;
  protected readonly listId = `${this.inputId}-suggestions`;
  protected readonly text = linkedSignal(() => this.value() ?? '');
  protected readonly open = signal(false);
  protected readonly active = signal(-1);

  /** The text asked for, a moment after the last keystroke. */
  private readonly asked = signal('');
  private debounce?: ReturnType<typeof setTimeout>;

  private readonly found = apiResource(listRepositories, () =>
    this.open()
      ? {
          q: this.asked() || undefined,
          sort: 'name' as const,
          // Ask for more where some will be left out, so the list stays full.
          limit: this.pullsOnly() ? MAX_SUGGESTIONS * 3 : MAX_SUGGESTIONS,
        }
      : undefined,
  );

  /** The server searches; while a new answer loads, the last one stays. */
  protected readonly suggestions = linkedSignal<Repository[] | undefined, Repository[]>({
    source: () => {
      const items = this.found.body()?.items;
      return items?.filter((r) => !this.pullsOnly() || r.pullRequests).slice(0, MAX_SUGGESTIONS);
    },
    computation: (fresh, prev) => fresh ?? prev?.value ?? [],
  });

  protected readonly showList = computed(() => {
    const s = this.suggestions();
    return this.open() && s.length > 0 && !(s.length === 1 && s[0].key === this.text());
  });

  protected type(v: string): void {
    this.text.set(v);
    clearTimeout(this.debounce);
    this.debounce = setTimeout(() => this.asked.set(v.trim()), 150);
    this.open.set(true);
    this.active.set(-1);
  }

  protected focus(): void {
    this.asked.set(this.text().trim() === this.value() ? '' : this.text().trim());
    this.open.set(true);
  }

  protected pick(r: Repository): void {
    this.text.set(r.key);
    this.open.set(false);
    this.active.set(-1);
    this.commit(r.key);
  }

  protected clear(): void {
    this.text.set('');
    this.asked.set('');
    this.commit('');
  }

  /**
   * Leaving the field: emptied, it removes the filter; a key typed in full
   * is taken; anything else goes back to the current filter.
   */
  protected leave(): void {
    this.open.set(false);
    this.active.set(-1);
    this.settle();
  }

  private settle(): void {
    const t = this.text().trim();
    if (!t) {
      this.commit('');
      return;
    }
    const hit = this.suggestions().find((r) => r.key.toLowerCase() === t.toLowerCase());
    if (hit) {
      this.text.set(hit.key);
      this.commit(hit.key);
    } else {
      this.text.set(this.value() ?? '');
    }
  }

  private commit(key: string): void {
    if (key !== (this.value() ?? '')) {
      this.committed.emit(key);
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
