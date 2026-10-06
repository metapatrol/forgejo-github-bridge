import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Repository } from '../api/models/repository';
import { RepositoryInput } from './repository-input';

function repo(key: string, pullRequests = true): Repository {
  return {
    key,
    github: key,
    forgejo: `platform/${key.split('/')[1]}`,
    githubUrl: '',
    forgejoUrl: '',
    pullRequests,
    targetBranches: [],
    rebaseRequests: false,
    importLabel: 'github',
    publishLabel: 'public',
    counts: { issuesOpen: 0, issuesClosed: 0, pullsOpen: 0, pullsMerged: 0, pullsClosed: 0 },
    queue: { depth: 0, failing: 0 },
    cursors: [],
  };
}

const flush = () => new Promise((r) => setTimeout(r, 200));

describe('RepositoryInput', () => {
  let http: HttpTestingController;
  let fixture: ComponentFixture<RepositoryInput>;
  let committed: string[];

  function setup(value = '', pullsOnly = false) {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    http = TestBed.inject(HttpTestingController);
    fixture = TestBed.createComponent(RepositoryInput);
    fixture.componentRef.setInput('value', value);
    fixture.componentRef.setInput('pullsOnly', pullsOnly);
    committed = [];
    fixture.componentInstance.committed.subscribe((k) => committed.push(k));
    fixture.detectChanges();
  }

  const input = () => (fixture.nativeElement as HTMLElement).querySelector('input')!;

  /** Types text and answers the search the field makes with the given repositories. */
  async function typeAndAnswer(text: string, answer: Repository[]) {
    input().dispatchEvent(new Event('focus'));
    input().value = text;
    input().dispatchEvent(new Event('input'));
    await flush();
    TestBed.inject(ApplicationRef).tick();
    const reqs = http.match((r) => r.url === '/api/v1/repositories');
    const last = reqs[reqs.length - 1];
    for (const r of reqs.slice(0, -1)) {
      r.flush({ items: [], total: 0 });
    }
    last.flush({ items: answer, total: answer.length });
    await fixture.whenStable();
    fixture.detectChanges();
    return last.request;
  }

  /** The suggestions as read: the label, and the hint beside it. */
  const options = () =>
    Array.from((fixture.nativeElement as HTMLElement).querySelectorAll('[role="option"]')).map(
      (o) =>
        Array.from(o.querySelectorAll('span'))
          .map((s) => s.textContent?.trim())
          .join(' '),
    );

  function press(key: string) {
    input().dispatchEvent(new KeyboardEvent('keydown', { key }));
    fixture.detectChanges();
  }

  it('asks the API with what is typed, and lists the answers with their Forgejo name', async () => {
    setup();
    const req = await typeAndAnswer('gate', [
      repo('metapatrol/gateway'),
      repo('metapatrol/gateway-ui'),
    ]);
    expect(req.params.get('q')).toBe('gate');
    expect(req.params.get('limit')).toBe('8');
    expect(req.params.has('pullRequests')).toBe(false);
    expect(options()).toEqual([
      'metapatrol/gateway platform/gateway',
      'metapatrol/gateway-ui platform/gateway-ui',
    ]);
    expect(input().getAttribute('aria-expanded')).toBe('true');
  });

  it('commits the suggestion chosen with the keyboard', async () => {
    setup();
    await typeAndAnswer('gate', [repo('metapatrol/gateway'), repo('metapatrol/gateway-ui')]);
    press('ArrowDown');
    press('ArrowDown');
    expect(input().getAttribute('aria-activedescendant')).toMatch(/-1$/);
    press('Enter');
    expect(committed).toEqual(['metapatrol/gateway-ui']);
  });

  it('removes the filter with the clear button', () => {
    setup('metapatrol/gateway');
    const clear = (fixture.nativeElement as HTMLElement).querySelector(
      'button[aria-label="Remove the repository filter"]',
    ) as HTMLButtonElement;
    clear.click();
    expect(committed).toEqual(['']);
  });

  it('removes the filter when the field is emptied', () => {
    setup('metapatrol/gateway');
    input().value = '';
    input().dispatchEvent(new Event('input'));
    input().dispatchEvent(new Event('blur'));
    expect(committed).toEqual(['']);
  });

  it('goes back to the current filter when what was typed is no repository', async () => {
    setup('metapatrol/gateway');
    await typeAndAnswer('nothing-like-it', []);
    input().dispatchEvent(new Event('blur'));
    fixture.detectChanges();
    expect(committed).toEqual([]);
    expect(input().value).toBe('metapatrol/gateway');
  });

  it('suggests only repositories that import pull requests, for the pull request list', async () => {
    setup('', true);
    const req = await typeAndAnswer('', [repo('metapatrol/bridge'), repo('metapatrol/gateway')]);
    expect(req.params.get('pullRequests')).toBe('true');
    expect(req.params.get('limit')).toBe('8');
    expect(options().map((o) => o?.split(' ')[0])).toEqual([
      'metapatrol/bridge',
      'metapatrol/gateway',
    ]);
  });
});
