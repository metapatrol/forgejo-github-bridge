import { inject } from '@angular/core';
import { CanActivateFn, RedirectCommand, Router } from '@angular/router';
import { PairKind } from '../api/models/pair-kind';
import { PairState } from '../api/models/pair-state';

/** The value of ?state= that asks for every state. */
export const ANY_STATE = 'any';

/** The states each list can be filtered by, besides any. */
export const STATES: Record<PairKind, readonly PairState[]> = {
  issue: ['open', 'closed'],
  pull: ['open', 'merged', 'closed'],
};

/** What an internal link to a list carries, so it never depends on the redirect. */
export const OPEN = { state: 'open' } as const;

/**
 * Lists of issues and pull requests always name their state in the URL:
 * without one, or with one the kind does not have, the router sends the
 * browser to state=open, keeping every other parameter. The list itself
 * then only reads the URL.
 */
export function requireState(kind: PairKind): CanActivateFn {
  return (route) => {
    const state = route.queryParamMap.get('state');
    if (state === ANY_STATE || STATES[kind].includes(state as PairState)) {
      return true;
    }
    const router = inject(Router);
    const path = route.pathFromRoot.flatMap((r) => r.url.map((s) => s.path));
    const tree = router.createUrlTree(['/', ...path], {
      queryParams: { ...route.queryParams, state: 'open' },
    });
    return new RedirectCommand(tree, { replaceUrl: true });
  };
}

/** What to ask the API for: every state for any, otherwise the URL's. */
export function apiState(state: string | null | undefined): PairState | undefined {
  return state === ANY_STATE || !state ? undefined : (state as PairState);
}
