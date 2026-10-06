import { Repository } from '../api/models/repository';

export interface RepoHealth {
  /** 0 failing, 1 queued, 2 idle: problems sort first. */
  rank: number;
  /** An mp-status value. */
  status: 'error' | 'active' | 'success';
  text: string;
}

/** A repository's queue in one word, with the dot that goes with it. */
export function repoHealth(r: Repository): RepoHealth {
  if (r.queue.failing > 0) {
    return { rank: 0, status: 'error', text: `${r.queue.failing} failing` };
  }
  if (r.queue.depth > 0) {
    return { rank: 1, status: 'active', text: `${r.queue.depth} queued` };
  }
  return { rank: 2, status: 'success', text: 'in step' };
}

/** When any of the repository's polls last moved. */
export function lastPoll(r: Repository): string | undefined {
  return r.cursors
    .map((c) => c.updatedAt)
    .sort()
    .at(-1);
}

/**
 * How to name repositories where space is short: without the owner when they
 * all share one, as owner/name otherwise.
 */
export function displayNames(repos: readonly Repository[]): (r: Repository) => string {
  const owners = new Set(repos.map((r) => r.key.split('/')[0]));
  if (owners.size !== 1) {
    return (r) => r.key;
  }
  return (r) => r.key.slice(r.key.indexOf('/') + 1);
}

/** From this many repositories on, the overview offers a filter; the API searches. */
export const FILTER_FROM = 10;
