import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ApplicationRef } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { provideApiConfiguration } from '../../api/api-configuration';
import { System as SystemModel } from '../../api/models/system';
import {
  cacheHitRatio,
  interval,
  lastOf,
  roundDuration,
  roundSummary,
  schemaLine,
  System,
} from './system';

const system: SystemModel = {
  service: {
    version: '0.4.0',
    commit: 'abc1234',
    builtAt: '2026-10-06T00:00:00Z',
    goVersion: 'go1.27.1',
    instance: 'bridge-0',
    mode: 'bridge',
    startedAt: '2026-10-06T00:00:00Z',
    pollIntervalSeconds: 60,
    fullResyncSeconds: 21600,
    repositories: 3,
    pullRequestRepositories: 2,
    leader: { present: true, instance: 'bridge-0' },
  },
  database: {
    version: '17.6 (Debian 17.6-1)',
    sizeBytes: 9_000_000,
    connections: 4,
    maxConnections: 100,
    poolSize: 3,
    poolInUse: 1,
    poolMax: 5,
    blocksHit: 990,
    blocksRead: 10,
    deadlocks: 0,
    tempBytes: 0,
    xidAge: 1000,
    freezeMaxAge: 200_000_000,
    settings: [{ name: 'autovacuum', value: 'on' }],
  },
  tables: [
    {
      name: 'issue_mapping',
      totalBytes: 65536,
      heapBytes: 8192,
      indexBytes: 57344,
      liveRows: 48,
      deadRows: 3,
      modifiedSinceAnalyze: 5,
      vacuumThreshold: 60,
      analyzeThreshold: 55,
      indexes: [
        { name: 'issue_mapping_pkey', method: 'btree', bytes: 16384, scans: 120, valid: true },
        { name: 'issue_mapping_broken', method: 'btree', bytes: 8192, scans: 0, valid: false },
      ],
    },
  ],
  migrations: [{ name: '0001_initial.sql', appliedAt: '2026-10-01T00:00:00Z' }],
  running: [],
  maintenance: {
    intervalSeconds: 86400,
    nextRoundAt: '2099-01-01T00:00:00Z',
    rounds: [
      {
        startedAt: '2026-10-06T00:00:00Z',
        finishedAt: '2026-10-06T00:00:02Z',
        tables: ['issue_mapping', 'dirty'],
        rebuilt: ['dirty_due'],
        dropped: [],
        failures: ['VACUUM poll_cursor: lock timeout'],
      },
      {
        startedAt: '2026-10-05T00:00:00Z',
        finishedAt: '2026-10-05T00:00:01Z',
        tables: ['issue_mapping'],
        rebuilt: [],
        dropped: [],
        failures: [],
      },
    ],
  },
};

describe('System', () => {
  it('computes what the page shows', () => {
    expect(cacheHitRatio({ blocksHit: 990, blocksRead: 10 })).toBe(0.99);
    expect(cacheHitRatio({ blocksHit: 0, blocksRead: 0 })).toBe(1);
    expect(interval(60)).toBe('1 min');
    expect(interval(21600)).toBe('6 h');
    expect(interval(45)).toBe('45 s');
    expect(lastOf(undefined, undefined)).toBeUndefined();
    expect(lastOf('2026-10-02T00:00:00Z', '2026-10-01T00:00:00Z')?.by).toBe('round');
    expect(lastOf(undefined, '2026-10-01T00:00:00Z')?.by).toBe('autovacuum');
    const round = system.maintenance.rounds[0];
    expect(roundSummary(round)).toBe('2 tables vacuumed · rebuilt dirty_due');
    expect(roundDuration(round)).toBe(2000);
    expect(roundDuration({ ...round, finishedAt: undefined })).toBeUndefined();
    const schema = schemaLine([
      { name: '0002_pull_requests.sql', appliedAt: '2026-10-02T00:00:00Z' },
      { name: '0001_initial.sql', appliedAt: '2026-10-01T00:00:00Z' },
    ]);
    expect(schema?.newest).toBe('0002_pull_requests');
    expect(schema?.count).toBe(2);
    expect(schema?.tip.split('\n').length).toBe(2);
    expect(schemaLine([])).toBeUndefined();
  });

  async function render(model: SystemModel): Promise<string> {
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideApiConfiguration('/api/v1'),
      ],
    });
    const fixture = TestBed.createComponent(System);
    fixture.detectChanges();
    TestBed.inject(ApplicationRef).tick();
    const http = TestBed.inject(HttpTestingController);
    http.expectOne('/api/v1/system').flush(model);
    http.expectOne('/api/v1/status').flush({
      version: '0.4.0',
      commit: 'abc1234',
      builtAt: '2026-10-06T00:00:00Z',
      instance: 'bridge-0',
      mode: 'bridge',
      forgejoUrl: 'https://forgejo.example.com',
      queue: { depth: 0, failing: 0 },
      githubRateLimits: [
        {
          resource: 'core',
          limit: 5000,
          remaining: 200,
          used: 4800,
          resetAt: '2099-01-01T00:00:00Z',
          observedAt: '2026-10-06T00:00:00Z',
        },
        {
          resource: 'search',
          limit: 30,
          remaining: 30,
          used: 0,
          resetAt: '2099-01-01T00:00:00Z',
          observedAt: '2026-10-06T00:00:00Z',
        },
      ],
    });
    await fixture.whenStable();
    fixture.detectChanges();
    meters = fixture.nativeElement.querySelectorAll('[role="meter"]').length;
    return ((fixture.nativeElement as HTMLElement).textContent ?? '').replace(/\s+/g, ' ');
  }

  let meters = 0;

  it('renders the service, the database, the tables and the migrations', async () => {
    const text = await render(system);
    expect(text).toContain('go1.27.1');
    expect(text).toContain('every 1 min');
    expect(text).toContain('2 with pull requests');
    expect(text).toContain('99 %');
    expect(text).toContain('issue_mapping');
    expect(text).toContain('1 invalid');
    expect(text).toContain('0001_initial · 1 migration');
    expect(text).not.toContain('Migrations');
    expect(text).not.toContain('every 30 s');
    expect(text).toContain('a round every 24 h');
    expect(text).toContain('took 2.0 s');
    expect(text).toContain('2 tables vacuumed · rebuilt dirty_due');
    expect(text).toContain('VACUUM poll_cursor: lock timeout');
    expect(text).toContain('2 recent rounds');
    expect(text).toContain('GitHub API');
    expect(text).toContain('4,800 of 5,000');
    expect(text).toContain('4.0 %');
    expect(text).toContain('search');
    // the freeze meter, two per table, and one per rate-limit window
    expect(meters).toBe(5);
  });

  it('shows a round that is running', async () => {
    const running = structuredClone(system);
    running.maintenance.rounds[0].finishedAt = undefined;
    const text = await render(running);
    expect(text).toContain('running now');
    expect(text).toContain('running since');
  });

  it('says when scheduled maintenance is off', async () => {
    const text = await render({
      ...system,
      maintenance: { intervalSeconds: 0, rounds: [] },
    });
    expect(text).toContain('scheduled maintenance is off');
    expect(text).toContain('none yet');
    expect(text).not.toContain('next round');
  });
});
