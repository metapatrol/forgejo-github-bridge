import { DestroyRef, effect, inject, signal, untracked } from '@angular/core';
import { Api, ApiFnOptional } from '../api/api';

/** Rows a page adds as the reader scrolls. */
export const PAGE = 50;

/** One answer of a paged list. */
export interface Page<T> {
  items: T[];
  total: number;
  nextCursor?: string;
}

/** What paging adds to a list's own parameters. */
export interface Paging {
  limit?: number;
  cursor?: string;
}

/**
 * A cursor-paged list: the first page, more pages appended as the reader
 * scrolls, and a refresh that keeps what is shown.
 *
 * A cursor is bound to the filters, the search and the order it was handed
 * out for, so a change of any of them starts again from the first page. A
 * refresh asks for as many rows as are shown, from the start and without a
 * cursor, so the rows that moved are where they are now and the list does
 * not jump back to its first page; paging then continues from that answer.
 * An answer that comes back after a newer request was made is dropped.
 *
 * Kept apart from Angular's resources so that it can be tested with any
 * fetch; pagedList() wires it to the API.
 */
export class Pager<P extends object, T> {
  readonly items = signal<T[]>([]);
  readonly total = signal(0);
  readonly nextCursor = signal<string | undefined>(undefined);
  readonly loading = signal(false);
  readonly error = signal<unknown>(undefined);
  /** Whether a first page has arrived for the current parameters. */
  readonly loaded = signal(false);

  private params: P | undefined;
  /** Counts requests; an answer for an older one is stale. */
  private generation = 0;

  constructor(
    private readonly fetch: (params: P & Paging) => Promise<Page<T>>,
    private readonly size = PAGE,
  ) {}

  /** Whether there are rows left to load. */
  more(): boolean {
    return !!this.nextCursor();
  }

  /** Starts again from the first page, for new parameters; undefined: nothing to load. */
  reset(params: P | undefined): void {
    this.params = params;
    this.items.set([]);
    this.total.set(0);
    this.nextCursor.set(undefined);
    this.loaded.set(false);
    this.error.set(undefined);
    if (params) {
      void this.load({ ...params, limit: this.size }, 'replace');
    } else {
      this.generation++;
    }
  }

  /** Appends the next page, if there is one and nothing is loading. */
  loadMore(): void {
    const cursor = this.nextCursor();
    if (!this.params || !cursor || this.loading()) {
      return;
    }
    void this.load({ ...this.params, limit: this.size, cursor }, 'append');
  }

  /** Loads what is shown again, from the start. Returns false when there is nothing to reload. */
  reload(): boolean {
    if (!this.params) {
      return false;
    }
    const shown = Math.max(this.items().length, this.size);
    void this.load({ ...this.params, limit: shown }, 'replace');
    return true;
  }

  private async load(params: P & Paging, mode: 'replace' | 'append'): Promise<void> {
    const generation = ++this.generation;
    this.loading.set(true);
    try {
      const page = await this.fetch(params);
      if (generation !== this.generation) {
        return;
      }
      this.items.set(mode === 'append' ? [...this.items(), ...page.items] : page.items);
      this.total.set(page.total);
      this.nextCursor.set(page.nextCursor || undefined);
      this.error.set(undefined);
      this.loaded.set(true);
    } catch (err) {
      if (generation === this.generation) {
        this.error.set(err);
      }
    } finally {
      if (generation === this.generation) {
        this.loading.set(false);
      }
    }
  }
}

/**
 * A Pager over one generated list function, starting again whenever params()
 * changes; call in an injection context.
 */
export function pagedList<P extends object, T>(
  fn: ApiFnOptional<P & Paging, Page<T>>,
  params: () => P | undefined,
  size = PAGE,
): Pager<P, T> {
  const api = inject(Api);
  const pager = new Pager<P, T>((p) => api.invoke(fn, p), size);
  const ref = effect(() => {
    const p = params();
    untracked(() => pager.reset(p));
  });
  inject(DestroyRef).onDestroy(() => ref.destroy());
  return pager;
}

/** ?sort= as a list takes it: one of its columns, either way round, or the list's default. */
export function sortParam<S extends string>(
  raw: string | undefined,
  columns: readonly string[],
): S | undefined {
  return raw && columns.includes(raw.replace(/^-/, '')) ? (raw as S) : undefined;
}
