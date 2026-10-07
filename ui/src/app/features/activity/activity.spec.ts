import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Activity } from './activity';

describe('Activity', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  /** The request the page makes, for the URL's parameters. */
  function request(inputs: Record<string, string>) {
    const fixture = TestBed.createComponent(Activity);
    for (const [k, v] of Object.entries(inputs)) {
      fixture.componentRef.setInput(k, v);
    }
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    return { fixture, req: http.expectOne((r) => r.url === '/api/v1/activity') };
  }

  it('asks for the newest first, a page at a time, without filters by default', () => {
    const { req } = request({});
    const p = req.request.params;
    expect(p.get('limit')).toBe('50');
    for (const name of ['repository', 'side', 'kind', 'action', 'sort', 'cursor']) {
      expect(p.has(name)).toBe(false);
    }
  });

  it('passes the filters and the order in the URL on', () => {
    const { req } = request({
      repository: 'metapatrol/foo',
      side: 'forgejo',
      kind: 'line comment',
      action: 'edit',
      sort: '-repository',
    });
    const p = req.request.params;
    expect(p.get('repository')).toBe('metapatrol/foo');
    expect(p.get('side')).toBe('forgejo');
    expect(p.get('kind')).toBe('line comment');
    expect(p.get('action')).toBe('edit');
    expect(p.get('sort')).toBe('-repository');
  });

  it('drops what it does not know', () => {
    const { req } = request({ side: 'gitlab', kind: 'wiki', action: 'merge', sort: 'size' });
    const p = req.request.params;
    for (const name of ['side', 'kind', 'action', 'sort']) {
      expect(p.has(name)).toBe(false);
    }
  });

  it('shows each entry as a sentence, linked to its pair and where it happened', async () => {
    const { fixture, req } = request({});
    req.flush({
      items: [
        {
          id: 1,
          at: '2026-10-07T00:00:00Z',
          repository: 'metapatrol/foo',
          pairId: 9,
          pairKind: 'issue',
          side: 'github',
          kind: 'issue',
          action: 'close',
          githubNumber: 3,
          url: 'https://github.com/metapatrol/foo/issues/3',
        },
      ],
      total: 1,
    });
    await fixture.whenStable();
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('tbody')?.textContent).toContain('closed GitHub #3');
    const links = Array.from(el.querySelectorAll('tbody a')).map((a) => a.getAttribute('href'));
    expect(links).toContain('/issues/9');
    expect(links).toContain('/repositories/metapatrol/foo');
    expect(links).toContain('https://github.com/metapatrol/foo/issues/3');
  });
});
