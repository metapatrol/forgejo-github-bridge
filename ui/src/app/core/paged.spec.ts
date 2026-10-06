import { Page, Pager, sortParam } from './paged';

/** A fetch that answers when told to, and records what it was asked. */
function controlledFetch() {
  const calls: { params: object; resolve: (p: Page<number>) => void }[] = [];
  const fetch = (params: object) =>
    new Promise<Page<number>>((resolve) => calls.push({ params, resolve }));
  return { calls, fetch };
}

const flush = () => new Promise((r) => setTimeout(r));

describe('Pager', () => {
  it('loads the first page, then appends the next with its cursor', async () => {
    const { calls, fetch } = controlledFetch();
    const pager = new Pager<{ q?: string }, number>(fetch, 2);
    pager.reset({ q: 'x' });
    expect(calls[0].params).toEqual({ q: 'x', limit: 2 });
    calls[0].resolve({ items: [1, 2], total: 5, nextCursor: 'c2' });
    await flush();
    expect(pager.items()).toEqual([1, 2]);
    expect(pager.more()).toBe(true);

    pager.loadMore();
    expect(calls[1].params).toEqual({ q: 'x', limit: 2, cursor: 'c2' });
    calls[1].resolve({ items: [3, 4], total: 5, nextCursor: 'c4' });
    await flush();
    expect(pager.items()).toEqual([1, 2, 3, 4]);
    expect(pager.total()).toBe(5);
  });

  it('does not ask for more while a page loads, or when there is no more', async () => {
    const { calls, fetch } = controlledFetch();
    const pager = new Pager<object, number>(fetch, 2);
    pager.reset({});
    pager.loadMore();
    expect(calls.length).toBe(1);
    calls[0].resolve({ items: [1], total: 1 });
    await flush();
    pager.loadMore();
    expect(calls.length).toBe(1);
    expect(pager.more()).toBe(false);
  });

  it('starts again from the first page when the parameters change, dropping late answers', async () => {
    const { calls, fetch } = controlledFetch();
    const pager = new Pager<{ sort?: string }, number>(fetch, 2);
    pager.reset({ sort: 'title' });
    pager.reset({ sort: '-title' });
    expect(pager.items()).toEqual([]);
    calls[1].resolve({ items: [9, 8], total: 2 });
    calls[0].resolve({ items: [1, 2], total: 2, nextCursor: 'stale' });
    await flush();
    expect(pager.items()).toEqual([9, 8]);
    expect(pager.more()).toBe(false);
    expect(calls[1].params).toEqual({ sort: '-title', limit: 2 });
  });

  it('reloads as many rows as are shown, from the start, then pages on from there', async () => {
    const { calls, fetch } = controlledFetch();
    const pager = new Pager<object, number>(fetch, 2);
    pager.reset({});
    calls[0].resolve({ items: [1, 2], total: 6, nextCursor: 'c2' });
    await flush();
    pager.loadMore();
    calls[1].resolve({ items: [3, 4], total: 6, nextCursor: 'c4' });
    await flush();

    expect(pager.reload()).toBe(true);
    expect(calls[2].params).toEqual({ limit: 4 });
    // Meanwhile one row went away and another came in.
    calls[2].resolve({ items: [0, 1, 3, 4], total: 6, nextCursor: 'c4b' });
    await flush();
    expect(pager.items()).toEqual([0, 1, 3, 4]);
    pager.loadMore();
    expect(calls[3].params).toEqual({ limit: 2, cursor: 'c4b' });
  });

  it('reloads at least a page when nothing was shown', () => {
    const { calls, fetch } = controlledFetch();
    const pager = new Pager<object, number>(fetch, 50);
    expect(pager.reload()).toBe(false);
    pager.reset({});
    pager.reload();
    expect(calls[1].params).toEqual({ limit: 50 });
  });

  it('keeps the rows when a load fails, and says so', async () => {
    const pager = new Pager<object, number>(() => Promise.reject(new Error('down')), 2);
    pager.reset({});
    await flush();
    expect(pager.error()).toBeInstanceOf(Error);
    expect(pager.loading()).toBe(false);
  });
});

describe('sortParam', () => {
  it('takes a known column either way round, and nothing else', () => {
    const cols = ['title', 'created'];
    expect(sortParam('title', cols)).toBe('title');
    expect(sortParam('-created', cols)).toBe('-created');
    expect(sortParam('size', cols)).toBeUndefined();
    expect(sortParam(undefined, cols)).toBeUndefined();
  });
});
