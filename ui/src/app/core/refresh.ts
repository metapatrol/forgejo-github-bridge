import { DestroyRef, effect, inject, Injectable, signal } from '@angular/core';

/** How often the live pages reload what they show. */
export const REFRESH_MS = 15_000;

const STORAGE_KEY = 'forgejo-github-bridge.refresh.paused';

function storedPaused(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'true';
  } catch {
    return false;
  }
}

/**
 * The console's auto-refresh: a tick every REFRESH_MS while it is not paused
 * and the page is visible. Pausing is remembered, so a page someone is
 * reading does not shift under them after a reload either.
 */
@Injectable({ providedIn: 'root' })
export class RefreshService {
  readonly paused = signal(storedPaused());
  /** Counts up on every refresh; pages reload what they show when it changes. */
  readonly tick = signal(0);
  /** When the last tick happened, for "updated 12s ago". */
  readonly lastTick = signal(Date.now());

  constructor() {
    setInterval(() => {
      if (!this.paused() && document.visibilityState === 'visible') {
        this.bump();
      }
    }, REFRESH_MS);
  }

  toggle(): void {
    const paused = !this.paused();
    this.paused.set(paused);
    try {
      localStorage.setItem(STORAGE_KEY, String(paused));
    } catch {
      // no storage: the choice lasts until the page is reloaded
    }
    if (!paused) {
      this.bump();
    }
  }

  private bump(): void {
    this.tick.update((t) => t + 1);
    this.lastTick.set(Date.now());
  }
}

/**
 * Reloads the given resources on every refresh tick. reload() keeps what a
 * resource shows while it loads again, so the page does not flicker.
 */
export function autoRefresh(...resources: { reload(): boolean }[]): void {
  const refresh = inject(RefreshService);
  let first = true;
  const ref = effect(() => {
    refresh.tick();
    if (first) {
      first = false;
      return;
    }
    for (const r of resources) {
      r.reload();
    }
  });
  inject(DestroyRef).onDestroy(() => ref.destroy());
}
