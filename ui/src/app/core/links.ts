import { Params } from '@angular/router';
import { PairKind } from '../api/models/pair-kind';
import { PairState } from '../api/models/pair-state';
import { Side } from '../api/models/side';
import { repoPath } from './format';
import { KINDS } from './kinds';
import { ANY_STATE } from './state-filter';

/**
 * Where a link goes, as RouterLink takes it:
 * [routerLink]="l.commands" [queryParams]="l.query".
 */
export interface Link {
  commands: (string | number)[];
  query: Params;
}

/** What a list of issues or pull requests can be narrowed to. */
export interface PairFilter {
  /** Always explicit: lists without one are redirected to the open ones. */
  state: PairState | typeof ANY_STATE;
  repository?: string;
  /** An author is a login on one side; the origin says which. */
  author?: string;
  origin?: Side;
}

/** The list of issues or pull requests, filtered; empty filters are left out of the URL. */
export function pairsLink(kind: PairKind, filter: PairFilter): Link {
  return { commands: [KINDS[kind].route], query: compact({ ...filter }) };
}

/** The queue, of one repository and/or only what fails. */
export function queueLink(filter: { repository?: string; failing?: boolean } = {}): Link {
  return {
    commands: ['/queue'],
    query: compact({ repository: filter.repository, failing: filter.failing || undefined }),
  };
}

/** A repository's page. */
export function repositoryLink(key: string): Link {
  return { commands: ['/repositories', ...repoPath(key)], query: {} };
}

function compact(params: Record<string, string | boolean | undefined>): Params {
  return Object.fromEntries(Object.entries(params).filter(([, v]) => v !== undefined && v !== ''));
}

/** Where a repository's counts go: its issues and pull requests by state, and its queue. */
export function repositoryLinks(key: string) {
  const issues = (state: PairState) => pairsLink('issue', { repository: key, state });
  const pulls = (state: PairState) => pairsLink('pull', { repository: key, state });
  return {
    page: repositoryLink(key),
    issuesOpen: issues('open'),
    issuesClosed: issues('closed'),
    pullsOpen: pulls('open'),
    pullsMerged: pulls('merged'),
    pullsClosed: pulls('closed'),
    queue: queueLink({ repository: key }),
    queueFailing: queueLink({ repository: key, failing: true }),
  };
}

/** Where a contributor's counts go: their issues and pull requests, every state. */
export function contributorLinks(login: string, side: Side) {
  const filter = { author: login, origin: side, state: ANY_STATE } as const;
  return { issues: pairsLink('issue', filter), pulls: pairsLink('pull', filter) };
}
