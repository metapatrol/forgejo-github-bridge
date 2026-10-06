import { TestBed } from '@angular/core/testing';
import { provideRouter, Router, withComponentInputBinding } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideApiConfiguration } from './api/api-configuration';
import { routes } from './app.routes';

describe('routes', () => {
  it('sends the former repositories list to the overview', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter(routes),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    const harness = await RouterTestingHarness.create();
    await harness.navigateByUrl('/repositories');
    expect(TestBed.inject(Router).url).toBe('/');
    expect(harness.routeNativeElement?.textContent).toContain('Overview');
  });

  it('sends the former people pages to the contributors', async () => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter(routes, withComponentInputBinding()),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    const harness = await RouterTestingHarness.create();
    const router = TestBed.inject(Router);
    await harness.navigateByUrl('/people');
    expect(router.url).toBe('/contributors');
    await harness.navigateByUrl('/people/github/alice');
    expect(router.url).toBe('/contributors/github/alice');
  });
});
