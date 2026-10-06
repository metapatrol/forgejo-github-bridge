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
});
