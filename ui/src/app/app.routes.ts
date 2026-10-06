import { Routes } from '@angular/router';
import { requireState } from './core/state-filter';

const title = (page: string) => `${page} · forgejo-github-bridge`;

export const routes: Routes = [
  {
    path: '',
    pathMatch: 'full',
    title: title('Overview'),
    loadComponent: () => import('./features/overview/overview').then((m) => m.Overview),
  },
  {
    path: 'repositories',
    title: title('Repositories'),
    loadComponent: () => import('./features/repositories/repositories').then((m) => m.Repositories),
  },
  {
    path: 'repositories/:owner/:name',
    title: title('Repository'),
    loadComponent: () =>
      import('./features/repositories/repository').then((m) => m.RepositoryDetail),
  },
  {
    path: 'issues',
    title: title('Issues'),
    data: { kind: 'issue' },
    canActivate: [requireState('issue')],
    runGuardsAndResolvers: 'paramsOrQueryParamsChange',
    loadComponent: () => import('./features/pairs/pairs').then((m) => m.Pairs),
  },
  {
    path: 'issues/:id',
    title: title('Issue'),
    data: { kind: 'issue' },
    loadComponent: () => import('./features/pairs/pair').then((m) => m.PairDetailPage),
  },
  {
    path: 'pulls',
    title: title('Pull requests'),
    data: { kind: 'pull' },
    canActivate: [requireState('pull')],
    runGuardsAndResolvers: 'paramsOrQueryParamsChange',
    loadComponent: () => import('./features/pairs/pairs').then((m) => m.Pairs),
  },
  {
    path: 'pulls/:id',
    title: title('Pull request'),
    data: { kind: 'pull' },
    loadComponent: () => import('./features/pairs/pair').then((m) => m.PairDetailPage),
  },
  // Before issues and pull requests had pages of their own.
  { path: 'pairs', redirectTo: '/issues?state=open' },
  {
    path: 'pairs/:id',
    loadComponent: () => import('./features/pairs/pair-redirect').then((m) => m.PairRedirect),
  },
  {
    path: 'queue',
    title: title('Queue'),
    loadComponent: () => import('./features/queue/queue').then((m) => m.Queue),
  },
  {
    path: 'system',
    title: title('System'),
    loadComponent: () => import('./features/system/system').then((m) => m.System),
  },
  {
    path: '**',
    title: title('Not found'),
    loadComponent: () => import('./features/not-found/not-found').then((m) => m.NotFound),
  },
];
