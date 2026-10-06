import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Status } from '../api/models/status';
import { EventsService } from './events';
import { StatusService } from './status';

const base: Status = {
  version: '0.4.0',
  commit: 'abc1234',
  builtAt: '2026-10-06T00:00:00Z',
  instance: 'bridge-0',
  mode: 'bridge',
  forgejoUrl: 'https://forgejo.example.com',
  // The bridge's own state does not show in the console's.
  leader: { present: false },
  queue: { depth: 4, failing: 2 },
  githubRateLimits: [],
};

describe('StatusService', () => {
  let http: HttpTestingController;
  let service: StatusService;
  let events: EventsService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    http = TestBed.inject(HttpTestingController);
    service = TestBed.inject(StatusService);
    events = TestBed.inject(EventsService);
  });

  async function answer(respond: (r: ReturnType<HttpTestingController['expectOne']>) => void) {
    TestBed.inject(ApplicationRef).tick();
    respond(http.expectOne('/api/v1/status'));
    await TestBed.inject(ApplicationRef).whenStable();
    return service.connection();
  }

  it('is connecting until the stream is open', async () => {
    const c = await answer((r) => r.flush(base));
    expect(c.text).toBe('connecting…');
  });

  it('is connected with the API answering and the stream open', async () => {
    events.state.set('live');
    const c = await answer((r) => r.flush(base));
    expect(c.text).toBe('connected');
    expect(c.status).toBe('success');
  });

  it('warns while the stream reconnects', async () => {
    events.state.set('reconnecting');
    const c = await answer((r) => r.flush(base));
    expect(c.text).toBe('reconnecting…');
    expect(c.status).toBe('warning');
    expect(c.detail).toBe('Live updates are interrupted; what is shown may not be current.');
  });

  it('is unreachable when the API does not answer', async () => {
    events.state.set('live');
    const c = await answer((r) => r.flush('down', { status: 503, statusText: 'Unavailable' }));
    expect(c.text).toBe('unreachable');
    expect(c.status).toBe('error');
  });
});
