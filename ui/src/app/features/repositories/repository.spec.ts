import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Repository } from '../../api/models/repository';
import { hrefs } from '../../../testing/links';
import { RepositoryDetail } from './repository';

const foo: Repository = {
  key: 'metapatrol/foo',
  github: 'metapatrol/foo',
  forgejo: 'platform/foo',
  githubUrl: 'https://github.com/metapatrol/foo',
  forgejoUrl: 'https://forgejo.example.com/platform/foo',
  pullRequests: true,
  targetBranches: ['develop'],
  rebaseRequests: true,
  importLabel: 'github',
  publishLabel: 'public',
  counts: { issuesOpen: 9, issuesClosed: 3, pullsOpen: 3, pullsMerged: 2, pullsClosed: 1 },
  queue: { depth: 4, failing: 2 },
  cursors: [],
};

describe('RepositoryDetail', () => {
  it('links its counts to the lists they count', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(RepositoryDetail);
    fixture.componentRef.setInput('owner', 'metapatrol');
    fixture.componentRef.setInput('name', 'foo');
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    http.expectOne((r) => r.url === '/api/v1/repositories').flush({ items: [foo], total: 1 });
    await new Promise((r) => setTimeout(r));
    TestBed.inject(ApplicationRef).tick();
    for (const r of http.match(() => true)) {
      r.flush({ items: [], total: 0 });
    }
    await fixture.whenStable();
    fixture.detectChanges();
    const links = hrefs(fixture.nativeElement as HTMLElement);
    for (const want of [
      '/issues?repository=metapatrol/foo&state=open',
      '/issues?repository=metapatrol/foo&state=closed',
      '/pulls?repository=metapatrol/foo&state=open',
      '/pulls?repository=metapatrol/foo&state=merged',
      '/pulls?repository=metapatrol/foo&state=closed',
      '/queue?repository=metapatrol/foo',
      '/queue?repository=metapatrol/foo&failing=true',
    ]) {
      expect(links).toContain(want);
    }
  });
});
