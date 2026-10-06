import { RateLimit } from '../api/models/rate-limit';

/** Below this share left, the budget shows as a warning; below LOW_BAD, as an error. */
export const LOW_WARN = 0.2;
export const LOW_BAD = 0.05;

/** The window that matters: "core", the REST API the bridge uses. */
export function coreLimit(limits: readonly RateLimit[] | undefined): RateLimit | undefined {
  return limits?.find((r) => r.resource === 'core');
}

/** The share of the window left, 0 to 1; 1 for a window without a limit. */
export function leftShare(r: Pick<RateLimit, 'limit' | 'remaining'>): number {
  return r.limit > 0 ? Math.max(0, r.remaining) / r.limit : 1;
}

/** How a window shows: ok, warn below 20 % left, bad below 5 %. */
export function rateTone(r: Pick<RateLimit, 'limit' | 'remaining'>): 'ok' | 'warn' | 'bad' {
  const left = leftShare(r);
  return left < LOW_BAD ? 'bad' : left < LOW_WARN ? 'warn' : 'ok';
}

/** "12 %", with a decimal below 10 %. */
export function percentLeft(r: Pick<RateLimit, 'limit' | 'remaining'>): string {
  const p = leftShare(r) * 100;
  return `${p < 10 ? p.toFixed(1) : Math.round(p)} %`;
}
