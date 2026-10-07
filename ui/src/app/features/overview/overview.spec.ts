import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { ActivityEntry } from '../../api/models/activity-entry';
import { Author } from '../../api/models/author';
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

const contributors: Author[] = Array.from({ length: 10 }, (_, i) => ({
  login: `contributor-${i}`,
  side: i === 9 ? 'forgejo' : 'github',
  issues: 10 - i,
  pulls: i % 3,
  lastAt: '2026-10-06T00:00:00Z',
}));

async function render(
  status: Status,
  repos: Repository[] = [],
  filter?: string,
  authors: Author[] = [],
  activity: ActivityEntry[] = [],
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
  http
    .expectOne((r) => r.url === '/api/v1/repositories' && !r.params.has('q'))
    .flush({ items: repos, total: repos.length });
  const top = http.expectOne((r) => r.url === '/api/v1/authors');
  expect(top.request.params.get('limit')).toBe('8');
  top.flush({ items: authors.slice(0, 8), total: authors.length });
  http
    .expectOne((r) => r.url === '/api/v1/activity')
    .flush({ items: activity, total: activity.length });
  // The filter is searched by the API; the test answers with the repositories that contain it.
  for (let i = 0; i < 5 && filter; i++) {
    await new Promise((r) => setTimeout(r));
    TestBed.inject(ApplicationRef).tick();
  }
  for (const r of http.match((r) => r.url === '/api/v1/repositories' && r.params.has('q'))) {
    const q = (r.request.params.get('q') ?? '').toLowerCase();
    const items = repos.filter((x) => x.github.includes(q) || x.forgejo.includes(q));
    r.flush({ items, total: items.length });
  }
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
    // and the counts inside them, to their own lists
    expect(hrefs).toContain('/issues?state=closed');
    expect(hrefs).toContain('/pulls?state=merged');
    expect(hrefs).toContain('/pulls?state=closed');
    expect(hrefs.filter((h) => h === '/queue?failing=true').length).toBe(2);
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

  it('links a repository tile by its name, and its counts to their lists', async () => {
    const el = await render(working, [repository('foo', 2)]);
    const tile = el.querySelector('app-repo-tile') as HTMLElement;
    const links = Array.from(tile.querySelectorAll('a')).map((a) =>
      decodeURIComponent(a.getAttribute('href') ?? ''),
    );
    expect(links[0]).toBe('/repositories/metapatrol/foo');
    expect(links).toContain('/queue?repository=metapatrol/foo&failing=true');
    expect(links).toContain('/issues?repository=metapatrol/foo&state=open');
    expect(links).toContain('/issues?repository=metapatrol/foo&state=closed');
    // links do not nest
    expect(tile.querySelector('a a')).toBeNull();
  });

  it('offers no filter for a few repositories', async () => {
    const el = await render(
      working,
      ['a', 'b', 'c'].map((n) => repository(n)),
    );
    expect(el.querySelector('mp-list-search')).toBeNull();
    expect(el.querySelectorAll('app-repo-tile').length).toBe(3);
  });

  it('has the API search many repositories, in its order', async () => {
    const many = Array.from({ length: 10 }, (_, i) => repository(`svc-${i}`));
    many.push(repository('gateway', 2), repository('gateway-ui'));
    const el = await render(working, many, 'gateway');
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

  it('names the eight most active contributors, linking to their pages', async () => {
    const el = await render(working, [], undefined, contributors);
    const links = Array.from(
      el.querySelectorAll('section[aria-labelledby="contributors-heading"] ul a'),
    ).map((a) => a.getAttribute('href'));
    expect(links.length).toBe(8);
    expect(links[0]).toBe('/contributors/github/contributor-0');
    expect(el.textContent).toContain('all contributors →');
  });

  it('shows the last eight things the bridge wrote', async () => {
    const recent: ActivityEntry[] = [
      {
        id: 1,
        at: '2026-10-07T00:00:00Z',
        repository: 'metapatrol/foo',
        pairId: 7,
        pairKind: 'pull',
        side: 'forgejo',
        kind: 'comment',
        action: 'create',
        githubNumber: 13,
        forgejoNumber: 113,
      },
    ];
    const el = await render(working, [], undefined, [], recent);
    const panel = el.querySelector('section[aria-labelledby="activity-heading"]') as HTMLElement;
    expect(panel.textContent).toContain('commented on Forgejo #113');
    const links = Array.from(panel.querySelectorAll('a')).map((a) => a.getAttribute('href'));
    expect(links).toContain('/activity');
    expect(links).toContain('/pulls/7');
  });
});
