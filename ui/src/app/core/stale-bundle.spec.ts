import { NavigationError } from '@angular/router';
import { describe, expect, it } from 'vitest';
import { isStaleBundle, reloadOnStaleBundle } from './stale-bundle';

function storage() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
  };
}

const stale = new NavigationError(
  1,
  '/issues?state=open',
  new TypeError(
    'Failed to fetch dynamically imported module: http://localhost:4200/chunk-GPOR2J7M.js',
  ),
);

describe('stale bundle', () => {
  it('recognises a chunk the build no longer has', () => {
    expect(isStaleBundle(stale.error)).toBe(true);
    expect(isStaleBundle(new Error('Http failure response for /api/v1/status: 500'))).toBe(false);
  });

  it('reloads once onto the address the navigation was going to', () => {
    const s = storage();
    const reloads: string[] = [];
    reloadOnStaleBundle(
      stale,
      s,
      (u) => reloads.push(u),
      () => 1_000_000,
    );
    expect(reloads).toEqual(['/issues?state=open']);
    // A second failure right after: the build itself is broken; no loop.
    reloadOnStaleBundle(
      stale,
      s,
      (u) => reloads.push(u),
      () => 1_010_000,
    );
    expect(reloads).toHaveLength(1);
    // Much later, a new deployment again: reload again.
    reloadOnStaleBundle(
      stale,
      s,
      (u) => reloads.push(u),
      () => 2_000_000,
    );
    expect(reloads).toHaveLength(2);
  });

  it('leaves other navigation errors alone', () => {
    const reloads: string[] = [];
    reloadOnStaleBundle(new NavigationError(1, '/x', new Error('nope')), storage(), (u) =>
      reloads.push(u),
    );
    expect(reloads).toEqual([]);
  });
});
