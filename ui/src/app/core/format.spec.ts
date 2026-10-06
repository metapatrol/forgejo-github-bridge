import { isFuture, repoPath, shortSha, until } from './format';

describe('format', () => {
  const now = Date.parse('2026-10-06T12:00:00Z');

  it('says when something is due', () => {
    expect(until('2026-10-06T11:59:00Z', now)).toBe('due');
    expect(until('2026-10-06T12:00:30Z', now)).toBe('in 30s');
    expect(until('2026-10-06T12:05:00Z', now)).toBe('in 5m');
    expect(until('2026-10-06T13:10:00Z', now)).toBe('in 1h 10m');
    expect(until(undefined, now)).toBe('—');
    expect(isFuture('2026-10-06T12:00:01Z', now)).toBe(true);
    expect(isFuture('2026-10-06T11:00:00Z', now)).toBe(false);
  });

  it('splits a repository key for a route', () => {
    expect(repoPath('metapatrol/foo')).toEqual(['metapatrol', 'foo']);
  });

  it('shortens a commit', () => {
    expect(shortSha('0123456789abcdef')).toBe('0123456');
    expect(shortSha('')).toBe('—');
  });
});
