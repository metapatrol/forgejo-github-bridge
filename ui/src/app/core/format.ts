import { Instant } from '@metapatrol/ng';

function millis(value: Instant): number | undefined {
  if (value === null || value === undefined || value === '') return undefined;
  const ms =
    value instanceof Date ? value.getTime() : typeof value === 'number' ? value : Date.parse(value);
  return Number.isNaN(ms) ? undefined : ms;
}

/**
 * When something is due, relative to now: "in 3m", "in 1h 5m", or "due" for
 * what is due already. The brand's ago() is for the past only.
 */
export function until(value: Instant, now: number = Date.now()): string {
  const ms = millis(value);
  if (ms === undefined) return '—';
  const seconds = Math.round((ms - now) / 1000);
  if (seconds <= 0) return 'due';
  if (seconds < 60) return `in ${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `in ${hours}h ${minutes % 60}m`;
  return `in ${Math.floor(hours / 24)}d`;
}

/** Whether a time is in the future. */
export function isFuture(value: Instant, now: number = Date.now()): boolean {
  const ms = millis(value);
  return ms !== undefined && ms > now;
}

/** "owner/name" split for a route: ['owner', 'name']. */
export function repoPath(key: string): string[] {
  const i = key.indexOf('/');
  return i < 0 ? [key] : [key.slice(0, i), key.slice(i + 1)];
}

/** A commit shortened to seven characters, as git does. */
export function shortSha(sha: string | null | undefined): string {
  return sha ? sha.slice(0, 7) : '—';
}
