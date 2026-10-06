import { Routes } from '@angular/router';

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
    path: 'pairs',
    title: title('Pairs'),
    loadComponent: () => import('./features/pairs/pairs').then((m) => m.Pairs),
  },
  {
    path: 'pairs/:id',
    title: title('Pair'),
    loadComponent: () => import('./features/pairs/pair').then((m) => m.PairDetailPage),
  },
  {
    path: 'queue',
    title: title('Queue'),
    loadComponent: () => import('./features/queue/queue').then((m) => m.Queue),
  },
  {
    path: '**',
    title: title('Not found'),
    loadComponent: () => import('./features/not-found/not-found').then((m) => m.NotFound),
  },
];
