import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { Repository } from '../api/models/repository';
import { RepoTile } from './repo-tile';

const repo: Repository = {
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

describe('RepoTile', () => {
  beforeEach(() => TestBed.configureTestingModule({ providers: [provideRouter([])] }));

  function render() {
    const fixture = TestBed.createComponent(RepoTile);
    fixture.componentRef.setInput('repo', repo);
    fixture.componentRef.setInput('name', 'foo');
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('links the repository and says what fails', () => {
    const el = render();
    expect(el.querySelector('a')?.getAttribute('href')).toBe('/repositories/metapatrol/foo');
    const text = el.textContent ?? '';
    expect(text).toContain('foo');
    expect(text).toContain('2 failing');
    expect(text).toContain('PRs 3 open · 2 merged · 1 closed');
  });
});
