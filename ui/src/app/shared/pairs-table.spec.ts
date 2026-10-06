import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Pair } from '../api/models/pair';
import { PairsTable } from './pairs-table';

const pull: Pair = {
  id: 7,
  repository: 'metapatrol/foo',
  kind: 'pull',
  origin: 'github',
  state: 'open',
  title: 'Fix the parser',
  author: 'alice',
  githubNumber: 12,
  forgejoNumber: 90,
  githubUrl: 'https://github.com/metapatrol/foo/pull/12',
  forgejoUrl: 'https://forgejo.example.com/platform/foo/pulls/90',
  comments: 2,
  createdAt: '2026-10-06T00:00:00Z',
  updatedAt: '2026-10-06T00:00:00Z',
  pull: {
    contribBranch: 'pr/12',
    importedHeadSha: '0123456789',
    headFrozen: true,
    touchesCi: true,
    rebaseRequested: false,
    outcomeReported: false,
    updatedAt: '2026-10-06T00:00:00Z',
  },
  queued: {
    repository: 'metapatrol/foo',
    side: 'github',
    number: 12,
    url: 'https://github.com/metapatrol/foo/pull/12',
    markedAt: '2026-10-06T00:00:00Z',
    notBefore: '2026-10-06T00:00:00Z',
    attempts: 3,
    lastError: 'HTTP 403 from Forgejo',
  },
};

describe('PairsTable', () => {
  beforeEach(() => TestBed.configureTestingModule({ providers: [provideRouter([])] }));

  it('links both sides and shows what is wrong', () => {
    const fixture = TestBed.createComponent(PairsTable);
    fixture.componentRef.setInput('pairs', [pull]);
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    const hrefs = Array.from(el.querySelectorAll('a')).map((a) => a.getAttribute('href'));
    expect(hrefs).toContain('https://github.com/metapatrol/foo/pull/12');
    expect(hrefs).toContain('https://forgejo.example.com/platform/foo/pulls/90');
    expect(hrefs).toContain('/pairs/7');
    const text = el.textContent ?? '';
    expect(text).toContain('frozen');
    expect(text).toContain('touches CI');
    expect(text).toContain('failing');
  });

  it('says when there is nothing', () => {
    const fixture = TestBed.createComponent(PairsTable);
    fixture.componentRef.setInput('pairs', []);
    fixture.componentRef.setInput('empty', 'No pair matches.');
    fixture.detectChanges();
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('No pair matches.');
  });
});
