import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { PairDetail } from '../../api/models/pair-detail';
import { hrefs } from '../../../testing/links';
import { PairDetailPage } from './pair';

const pull: PairDetail = {
  id: 7,
  repository: 'metapatrol/foo',
  kind: 'pull',
  origin: 'github',
  state: 'merged',
  title: 'Fix the parser',
  author: 'alice',
  githubNumber: 13,
  forgejoNumber: 113,
  githubUrl: 'https://github.com/metapatrol/foo/pull/13',
  forgejoUrl: 'https://forgejo.example.com/platform/foo/pulls/113',
  comments: 0,
  createdAt: '2026-10-01T10:00:00Z',
  updatedAt: '2026-10-03T10:00:00Z',
  commentPairs: [],
  pull: {
    contribBranch: 'pr/13',
    importedHeadSha: '3a5f1c0aa',
    headFrozen: false,
    touchesCi: false,
    rebaseRequested: false,
    outcomeReported: true,
    updatedAt: '2026-10-02T12:00:00Z',
  },
  queued: {
    repository: 'metapatrol/foo',
    side: 'github',
    number: 13,
    url: '',
    markedAt: '2026-10-03T00:00:00Z',
    notBefore: '2026-10-04T00:00:00Z',
    attempts: 2,
    lastError: 'boom',
  },
};

describe('PairDetailPage', () => {
  it('links its repository, its state and its queue entry', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    const http = TestBed.inject(HttpTestingController);
    const fixture = TestBed.createComponent(PairDetailPage);
    fixture.componentRef.setInput('id', 7);
    fixture.componentRef.setInput('kind', 'pull');
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    http.expectOne('/api/v1/pairs/7').flush(pull);
    const act = http.expectOne((r) => r.url === '/api/v1/activity');
    expect(act.request.params.get('pair')).toBe('7');
    act.flush({
      items: [
        {
          id: 5,
          at: '2026-10-02T09:00:00Z',
          repository: 'metapatrol/foo',
          pairId: 7,
          pairKind: 'pull',
          side: 'forgejo',
          kind: 'pull',
          action: 'push',
          githubNumber: 13,
          forgejoNumber: 113,
          url: 'https://forgejo.example.com/github-contrib/foo/src/branch/pr/13',
        },
      ],
      total: 1,
    });
    await fixture.whenStable();
    fixture.detectChanges();
    const links = hrefs(fixture.nativeElement as HTMLElement);
    expect(links).toContain('/repositories/metapatrol/foo');
    expect(links).toContain('/pulls?repository=metapatrol/foo&state=merged');
    expect(links).toContain('/queue?repository=metapatrol/foo&failing=true');
    expect(links).toContain('/contributors/github/alice');
    // what the bridge wrote is part of the history
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('pushed to pr/13');
  });
});
