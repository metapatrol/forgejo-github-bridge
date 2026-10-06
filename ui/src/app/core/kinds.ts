import { PairKind } from '../api/models/pair-kind';

/** What each kind of pair is called, and where its pages are. */
export const KINDS: Record<PairKind, { route: string; one: string; many: string }> = {
  issue: { route: '/issues', one: 'issue', many: 'issues' },
  pull: { route: '/pulls', one: 'pull request', many: 'pull requests' },
};

/** The detail page of a pair, as router commands. */
export function pairLink(kind: PairKind, id: number): (string | number)[] {
  return [KINDS[kind].route, id];
}
