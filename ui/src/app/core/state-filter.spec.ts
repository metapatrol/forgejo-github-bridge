import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { ANY_STATE, apiState, requireState } from './state-filter';

@Component({ template: '' })
class List {}

describe('state in the URL', () => {
  let harness: RouterTestingHarness;
  let router: Router;

  beforeEach(async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([
          { path: 'issues', component: List, canActivate: [requireState('issue')] },
          { path: 'pulls', component: List, canActivate: [requireState('pull')] },
        ]),
      ],
    });
    harness = await RouterTestingHarness.create();
    router = TestBed.inject(Router);
  });

  it('sends a list without a state to the open ones, keeping the other parameters', async () => {
    await harness.navigateByUrl('/issues?repository=metapatrol%2Ffoo&q=crash');
    const params = router.parseUrl(router.url).queryParams;
    expect(params).toEqual({ repository: 'metapatrol/foo', q: 'crash', state: 'open' });
  });

  it('leaves a state the kind has, and any', async () => {
    await harness.navigateByUrl('/pulls?state=merged');
    expect(router.url).toBe('/pulls?state=merged');
    await harness.navigateByUrl('/issues?state=any');
    expect(router.url).toBe('/issues?state=any');
  });

  it('replaces a state the kind does not have', async () => {
    await harness.navigateByUrl('/issues?state=merged');
    expect(router.url).toBe('/issues?state=open');
  });

  it('asks the API for every state only for any', () => {
    expect(apiState(ANY_STATE)).toBeUndefined();
    expect(apiState('closed')).toBe('closed');
  });
});
