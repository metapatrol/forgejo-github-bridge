import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Repository } from '../../api/models/repository';
import { Status } from '../../api/models/status';
import { Overview } from './overview';

const working: Status = {
  version: '0.4.0',
  commit: 'abc1234',
  builtAt: '2026-10-06T00:00:00Z',
  instance: 'bridge-1',
  mode: 'bridge',
  forgejoUrl: 'https://forgejo.example.com',
  leader: { present: true, instance: 'bridge-0', connectedAt: '2026-10-06T00:00:00Z' },
  queue: { depth: 3, failing: 1 },
  githubRateLimits: [
    {
      resource: 'core',
      limit: 5000,
      remaining: 4321,
      used: 679,
      resetAt: '2099-01-01T00:00:00Z',
      observedAt: '2026-10-06T00:00:00Z',
    },
  ],
};

function repository(name: string, failing = 0): Repository {
  return {
    key: `metapatrol/${name}`,
    github: `metapatrol/${name}`,
    forgejo: `platform/${name}-src`,
    githubUrl: '',
    forgejoUrl: '',
    pullRequests: false,
    targetBranches: [],
    rebaseRequests: false,
    importLabel: 'github',
    publishLabel: 'public',
    counts: { issuesOpen: 0, issuesClosed: 0, pullsOpen: 0, pullsMerged: 0, pullsClosed: 0 },
    queue: { depth: failing, failing },
    cursors: [],
  };
}

async function render(
  status: Status,
  repos: Repository[] = [],
  filter?: string,
): Promise<HTMLElement> {
  TestBed.configureTestingModule({
    providers: [
      provideRouter([]),
      provideHttpClient(),
      provideHttpClientTesting(),
      provideApiConfiguration('/api/v1'),
    ],
  });
  const fixture = TestBed.createComponent(Overview);
  if (filter !== undefined) {
    fixture.componentRef.setInput('repo', filter);
  }
  fixture.detectChanges();
  TestBed.inject(ApplicationRef).tick();
  const http = TestBed.inject(HttpTestingController);
  http.expectOne('/api/v1/status').flush(status);
  http.expectOne('/api/v1/repositories').flush(repos);
  await fixture.whenStable();
  fixture.detectChanges();
  return fixture.nativeElement as HTMLElement;
}

describe('Overview', () => {
  it('shows what is left of the GitHub rate limit', async () => {
    const el = await render(working);
    const text = el.textContent ?? '';
    expect(text).toContain('4,321');
    expect(text).toContain('left of 5,000 · 86 %');
    expect(text).toContain('resets in');
    expect(el.querySelector('[role="meter"]')?.getAttribute('aria-valuenow')).toBe('679');
  });

  it('leaves failing entries to the queue tile, without a notice', async () => {
    const el = await render(working);
    expect(el.querySelector('mp-notice')).toBeNull();
    expect(el.textContent).toContain('1 failing');
  });

  it('says so before the first request', async () => {
    const text = (await render({ ...working, githubRateLimits: [] })).textContent ?? '';
    expect(text).toContain('no requests yet');
  });

  it('links every tile to its page', async () => {
    // queue, GitHub (the system page), issues and pull requests; no bridge tile
    const el = await render(working);
    const hrefs = Array.from(el.querySelectorAll('a')).map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('/system');
    expect(hrefs).toContain('/queue?failing=true');
    expect(hrefs).toContain('/issues?state=open');
    expect(hrefs).toContain('/pulls?state=open');
  });

  it('warns when no instance holds the leader lock', async () => {
    const el = await render({
      ...working,
      leader: { present: false },
      queue: { depth: 0, failing: 0 },
    });
    const text = el.textContent ?? '';
    expect(text).toContain('No instance holds the leader lock: nothing is being bridged.');
    expect(el.querySelector('mp-notice a')?.getAttribute('href')).toBe('/system');
    expect(Array.from(el.querySelectorAll('a')).map((a) => a.getAttribute('href'))).toContain(
      '/queue',
    );
  });

  it('offers no filter for a few repositories', async () => {
    const el = await render(
      working,
      ['a', 'b', 'c'].map((n) => repository(n)),
    );
    expect(el.querySelector('mp-list-search')).toBeNull();
    expect(el.querySelectorAll('app-repo-tile').length).toBe(3);
  });

  it('filters many repositories by ?repo=, problems first', async () => {
    const many = Array.from({ length: 10 }, (_, i) => repository(`svc-${i}`));
    many.push(repository('gateway', 2), repository('gateway-ui'));
    const el = await render(working, many, 'GATEWAY');
    expect(el.querySelector('mp-list-search')).not.toBeNull();
    const names = Array.from(el.querySelectorAll('app-repo-tile')).map((t) =>
      t.querySelector('a')?.getAttribute('href'),
    );
    expect(names).toEqual([
      '/repositories/metapatrol/gateway',
      '/repositories/metapatrol/gateway-ui',
    ]);
    expect(el.textContent).toContain('2 of 12');
  });

  it('matches the Forgejo name too', async () => {
    const many = Array.from({ length: 10 }, (_, i) => repository(`svc-${i}`));
    const el = await render(working, many, 'svc-3-src');
    expect(el.querySelectorAll('app-repo-tile').length).toBe(1);
  });
});
