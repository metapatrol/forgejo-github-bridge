import { Injectable, signal } from '@angular/core';

const STORAGE_KEY = 'forgejo-github-bridge.live.paused';

function storedPaused(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === 'true';
  } catch {
    return false;
  }
}

/**
 * Whether the console follows changes as they happen. Pausing is
 * remembered, so a page someone is reading does not shift under them after
 * a reload either.
 */
@Injectable({ providedIn: 'root' })
export class RefreshService {
  readonly paused = signal(storedPaused());

  toggle(): void {
    const paused = !this.paused();
    this.paused.set(paused);
    try {
      localStorage.setItem(STORAGE_KEY, String(paused));
    } catch {
      // no storage: the choice lasts until the page is reloaded
    }
  }
}
