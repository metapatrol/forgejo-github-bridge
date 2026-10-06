import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Author } from '../../api/models/author';
import { Contributors } from './contributors';
import { ContributorDetail } from './contributor';

const authors: Author[] = [
  {
    login: 'alice',
    side: 'github',
    profileUrl: 'https://github.com/alice',
    issues: 5,
    pulls: 2,
    lastAt: '2026-10-06T00:00:00Z',
  },
  { login: 'jdoe', side: 'forgejo', issues: 3, pulls: 0, lastAt: '2026-10-05T00:00:00Z' },
];

function setup() {
  TestBed.configureTestingModule({
    providers: [
      provideRouter([]),
      provideHttpClient(),
      provideHttpClientTesting(),
      provideApiConfiguration('/api/v1'),
    ],
  });
  return TestBed.inject(HttpTestingController);
}

describe('Contributors', () => {
  it('lists everyone, linked to their pages', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(Contributors);
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    const req = http.expectOne((r) => r.url === '/api/v1/authors');
    expect(req.request.params.get('limit')).toBe('50');
    req.flush({ items: authors, total: authors.length });
    await fixture.whenStable();
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    const names = Array.from(el.querySelectorAll('tbody app-author a')).map((a) =>
      a.getAttribute('href'),
    );
    expect(names).toEqual(['/contributors/github/alice', '/contributors/forgejo/jdoe']);
    // their counts link to their pairs, every state; a Forgejo login has no pull requests
    const counts = Array.from(el.querySelectorAll('tbody td.mp-num a')).map((a) =>
      a.getAttribute('href'),
    );
    expect(counts).toEqual([
      '/issues?author=alice&origin=github&state=any',
      '/pulls?author=alice&origin=github&state=any',
      '/issues?author=jdoe&origin=forgejo&state=any',
    ]);
    expect(el.textContent).toContain('Forgejo');
  });
});

describe('ContributorDetail', () => {
  it('shows the counts and their issues and pull requests, filtered by them', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(ContributorDetail);
    fixture.componentRef.setInput('side', 'github');
    fixture.componentRef.setInput('login', 'alice');
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    http.expectOne('/api/v1/authors').flush({ items: authors, total: authors.length });
    const pairs = http.match((r) => r.url === '/api/v1/pairs');
    expect(pairs.map((r) => r.request.params.get('kind')).sort()).toEqual(['issue', 'pull']);
    for (const r of pairs) {
      expect(r.request.params.get('author')).toBe('alice');
      expect(r.request.params.get('origin')).toBe('github');
      expect(r.request.params.get('state')).toBeNull();
      r.flush({ items: [], total: 0 });
    }
    await fixture.whenStable();
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.textContent?.replace(/\s+/g, ' ')).toContain('5 issues · 2 pull requests');
    expect(el.querySelector('a[href="https://github.com/alice"]')).not.toBeNull();
    const all = Array.from(el.querySelectorAll('a')).map((a) => a.getAttribute('href'));
    expect(all).toContain('/issues?author=alice&origin=github&state=any');
    // the counts in the panel link there too
    const counts = Array.from(el.querySelectorAll('dd a')).map((a) => a.getAttribute('href'));
    expect(counts).toContain('/issues?author=alice&origin=github&state=any');
    expect(counts).toContain('/pulls?author=alice&origin=github&state=any');
    expect(all).toContain('/pulls?author=alice&origin=github&state=any');
  });

  it('has no pull requests from Forgejo', async () => {
    const http = setup();
    const fixture = TestBed.createComponent(ContributorDetail);
    fixture.componentRef.setInput('side', 'forgejo');
    fixture.componentRef.setInput('login', 'jdoe');
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    http.expectOne('/api/v1/authors').flush({ items: authors, total: authors.length });
    const pairs = http.match((r) => r.url === '/api/v1/pairs');
    expect(pairs.map((r) => r.request.params.get('kind'))).toEqual(['issue']);
  });
});
