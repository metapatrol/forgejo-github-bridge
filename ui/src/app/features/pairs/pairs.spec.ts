import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { Pairs } from './pairs';

describe('Pairs', () => {
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

  /** The state the list asks the API for, given the URL's ?state=. */
  function requestedState(kind: 'issue' | 'pull', state?: string): string | null {
    const fixture = TestBed.createComponent(Pairs);
    fixture.componentRef.setInput('kind', kind);
    if (state !== undefined) {
      fixture.componentRef.setInput('state', state);
    }
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    const req = http.expectOne((r) => r.url === '/api/v1/pairs');
    expect(req.request.params.get('kind')).toBe(kind);
    return req.request.params.get('state');
  }

  it('asks for what the URL names', () => {
    expect(requestedState('issue', 'open')).toBe('open');
    expect(requestedState('pull', 'merged')).toBe('merged');
  });

  it('lists every state for state=any', () => {
    expect(requestedState('pull', 'any')).toBeNull();
  });

  it('filters by author, with the origin their login is on', () => {
    const fixture = TestBed.createComponent(Pairs);
    fixture.componentRef.setInput('kind', 'pull');
    fixture.componentRef.setInput('state', 'any');
    fixture.componentRef.setInput('author', 'alice');
    fixture.componentRef.setInput('origin', 'github');
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    const req = http.expectOne((r) => r.url === '/api/v1/pairs');
    expect(req.request.params.get('author')).toBe('alice');
    expect(req.request.params.get('origin')).toBe('github');
    fixture.detectChanges();
    const chip = (fixture.nativeElement as HTMLElement).querySelector(
      'button[aria-label="Remove the filter by alice"]',
    );
    expect(chip).not.toBeNull();
  });

  /** The request the list makes, for the URL's parameters. */
  function request(inputs: Record<string, string>) {
    const fixture = TestBed.createComponent(Pairs);
    fixture.componentRef.setInput('kind', 'issue');
    fixture.componentRef.setInput('state', 'any');
    for (const [k, v] of Object.entries(inputs)) {
      fixture.componentRef.setInput(k, v);
    }
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    return { fixture, req: http.expectOne((r) => r.url === '/api/v1/pairs') };
  }

  it('passes ?q= and ?sort= on, a page of 50 at a time', () => {
    const { req } = request({ q: 'parser', sort: '-title' });
    expect(req.request.params.get('q')).toBe('parser');
    expect(req.request.params.get('sort')).toBe('-title');
    expect(req.request.params.get('limit')).toBe('50');
    expect(req.request.params.has('cursor')).toBe(false);
    expect(req.request.params.has('offset')).toBe(false);
  });

  it('leaves an order it does not know to the API default', () => {
    const { req } = request({ sort: 'size' });
    expect(req.request.params.has('sort')).toBe(false);
  });

  it('starts again from the first page when the order changes', () => {
    const { fixture, req } = request({ sort: 'title' });
    req.flush({ items: [], total: 120, nextCursor: 'c50' });
    fixture.componentRef.setInput('sort', '-title');
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    const next = http.expectOne((r) => r.url === '/api/v1/pairs');
    expect(next.request.params.get('sort')).toBe('-title');
    expect(next.request.params.has('cursor')).toBe(false);
  });
});
