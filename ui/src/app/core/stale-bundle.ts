import { NavigationError } from '@angular/router';

/** Where the last reload for a stale bundle is remembered. */
const KEY = 'forgejo-github-bridge.stale-bundle-reload';

/** How long a reload for a stale bundle counts as just done. */
const WINDOW_MS = 60_000;

/**
 * Whether a navigation failed because the page still runs an older build:
 * a new version was deployed (or rebuilt) while the tab was open, and the
 * chunk the old bundle asks for is gone.
 */
export function isStaleBundle(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return /dynamically imported module|Importing a module script failed|ChunkLoadError|Loading chunk/i.test(
    message,
  );
}

/**
 * The router's navigation error handler: a stale bundle reloads the page
 * once, onto the current build, at the address the navigation was going to.
 * A second failure within a minute is left alone, so that a build that is
 * broken does not reload forever.
 */
export function reloadOnStaleBundle(
  e: NavigationError,
  storage: Pick<Storage, 'getItem' | 'setItem'> | undefined = safeSession(),
  reload: (url: string) => void = (url) => location.assign(url),
  now: () => number = Date.now,
): void {
  if (!isStaleBundle(e.error)) {
    return;
  }
  const last = Number(storage?.getItem(KEY) ?? 0);
  if (now() - last < WINDOW_MS) {
    return;
  }
  storage?.setItem(KEY, String(now()));
  reload(e.url);
}

function safeSession(): Storage | undefined {
  try {
    return sessionStorage;
  } catch {
    return undefined;
  }
}
