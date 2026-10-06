import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideApiConfiguration } from '../api/api-configuration';
import { Status } from '../api/models/status';
import { StatusService } from './status';

const base: Status = {
  version: '0.4.0',
  commit: 'abc1234',
  builtAt: '2026-10-06T00:00:00Z',
  instance: 'bridge-0',
  mode: 'bridge',
  forgejoUrl: 'https://forgejo.example.com',
  leader: { present: true, instance: 'bridge-0' },
  queue: { depth: 0, failing: 0 },
};

describe('StatusService', () => {
  let http: HttpTestingController;
  let service: StatusService;

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
  });

  async function answer(status: Status) {
    TestBed.inject(ApplicationRef).tick();
    http.expectOne('/api/v1/status').flush(status);
    await TestBed.inject(ApplicationRef).whenStable();
    return service.health();
  }

  it('is bridging when a leader works and nothing fails', async () => {
    expect((await answer(base)).text).toBe('bridging');
  });

  it('says so when nothing holds the leader lock', async () => {
    const h = await answer({ ...base, leader: { present: false } });
    expect(h.text).toBe('no leader');
    expect(h.status).toBe('error');
  });

  it('counts what fails', async () => {
    const h = await answer({ ...base, queue: { depth: 4, failing: 2 } });
    expect(h.text).toBe('2 failing');
    expect(h.status).toBe('warning');
  });
});
