import { coreLimit, leftShare, percentLeft, rateTone } from './rate-limit';

const window = (remaining: number, limit = 5000, resource = 'core') => ({
  resource,
  limit,
  remaining,
  used: limit - remaining,
  resetAt: '2026-10-06T13:00:00Z',
  observedAt: '2026-10-06T12:00:00Z',
});

describe('rate limit', () => {
  it('picks the core window', () => {
    expect(coreLimit([window(10, 60, 'search'), window(4000)])?.resource).toBe('core');
    expect(coreLimit([])).toBeUndefined();
    expect(coreLimit(undefined)).toBeUndefined();
  });

  it('warns below 20 % left and alarms below 5 %', () => {
    expect(rateTone(window(1000))).toBe('ok');
    expect(rateTone(window(999))).toBe('warn');
    expect(rateTone(window(250))).toBe('warn');
    expect(rateTone(window(249))).toBe('bad');
    expect(rateTone(window(0))).toBe('bad');
  });

  it('says how much is left', () => {
    expect(leftShare(window(2500))).toBe(0.5);
    expect(leftShare({ limit: 0, remaining: 0 })).toBe(1);
    expect(percentLeft(window(2500))).toBe('50 %');
    expect(percentLeft(window(150))).toBe('3.0 %');
  });
});
