import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { provideApiConfiguration } from '../api/api-configuration';
import { Author } from '../api/models/author';
import { AuthorFilter, AuthorInput } from './author-input';
import { PairFilters } from './pair-filters';

const alice: Author = { login: 'alice', side: 'github', issues: 3, pulls: 2, lastAt: '' };
const jdoe: Author = { login: 'jdoe', side: 'forgejo', issues: 6, pulls: 0, lastAt: '' };

const settle = () => new Promise((r) => setTimeout(r, 200));

describe('AuthorInput', () => {
  let http: HttpTestingController;
  let fixture: ComponentFixture<AuthorInput>;
  let committed: (AuthorFilter | null)[];

  function setup(kind: 'issue' | 'pull', author = '', origin?: 'github' | 'forgejo') {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    http = TestBed.inject(HttpTestingController);
    fixture = TestBed.createComponent(AuthorInput);
    fixture.componentRef.setInput('kind', kind);
    fixture.componentRef.setInput('author', author);
    fixture.componentRef.setInput('origin', origin);
    committed = [];
    fixture.componentInstance.committed.subscribe((f) => committed.push(f));
    fixture.detectChanges();
  }

  const field = () => (fixture.nativeElement as HTMLElement).querySelector('input')!;

  async function typeAndAnswer(text: string, answer: Author[]) {
    field().dispatchEvent(new Event('focus'));
    field().value = text;
    field().dispatchEvent(new Event('input'));
    await settle();
    TestBed.inject(ApplicationRef).tick();
    const reqs = http.match((r) => r.url === '/api/v1/authors');
    for (const r of reqs.slice(0, -1)) {
      r.flush({ items: [], total: 0 });
    }
    const last = reqs[reqs.length - 1];
    last.flush({ items: answer, total: answer.length });
    await settle();
    fixture.detectChanges();
    return last.request;
  }

  function press(key: string) {
    field().dispatchEvent(new KeyboardEvent('keydown', { key }));
    fixture.detectChanges();
  }

  it('asks for the people who opened issues, on either side, with their side beside them', async () => {
    setup('issue');
    const req = await typeAndAnswer('j', [jdoe]);
    expect(req.params.get('kind')).toBe('issue');
    expect(req.params.has('side')).toBe(false);
    expect(req.params.get('q')).toBe('j');
    expect(req.params.get('limit')).toBe('8');
    const option = (fixture.nativeElement as HTMLElement).querySelector('[role="option"]')!;
    expect(option.textContent).toContain('Forgejo');
  });

  it('asks only for GitHub logins on the pull request list', async () => {
    setup('pull');
    const req = await typeAndAnswer('a', [alice]);
    expect(req.params.get('kind')).toBe('pull');
    expect(req.params.get('side')).toBe('github');
  });

  it('commits the login together with its side', async () => {
    setup('issue');
    await typeAndAnswer('', [alice, jdoe]);
    press('ArrowDown');
    press('ArrowDown');
    press('Enter');
    expect(committed).toEqual([{ author: 'jdoe', origin: 'forgejo' }]);
  });

  it('clears with its button', () => {
    setup('issue', 'alice', 'github');
    (
      (fixture.nativeElement as HTMLElement).querySelector(
        'button[aria-label="Remove the author filter"]',
      ) as HTMLButtonElement
    ).click();
    expect(committed).toEqual([null]);
  });
});

describe('PairFilters', () => {
  it('sets the author with its origin, and clearing removes both', () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    const router = TestBed.inject(Router);
    const navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    const fixture = TestBed.createComponent(PairFilters);
    fixture.componentRef.setInput('kind', 'issue');
    fixture.detectChanges();
    const author = fixture.debugElement.query((d) => d.componentInstance instanceof AuthorInput)
      .componentInstance as AuthorInput;
    author.committed.emit({ author: 'jdoe', origin: 'forgejo' });
    expect(navigate.mock.calls[0][1]?.queryParams).toEqual({ author: 'jdoe', origin: 'forgejo' });
    author.committed.emit(null);
    expect(navigate.mock.calls[1][1]?.queryParams).toEqual({ author: null, origin: null });
  });
});
