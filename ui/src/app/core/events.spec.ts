import { BridgeEvent, concerns, debounce, parseEvent, Scope } from './events';

const pair = (repository?: string, pairKind?: 'issue' | 'pull', id = 7): BridgeEvent => ({
  kind: 'pair',
  op: 'update',
  id,
  repository,
  pairKind,
});
const queue = (repository = 'o/a'): BridgeEvent => ({
  kind: 'queue',
  op: 'insert',
  repository,
  side: 'github',
  number: 4,
});
const cursor = (repository = 'o/a'): BridgeEvent => ({
  kind: 'cursor',
  op: 'update',
  repository,
  side: 'github',
  stream: 'issues',
});
const leader: BridgeEvent = { kind: 'leader', present: false };
const rateLimit: BridgeEvent = { kind: 'rateLimit', op: 'update', resource: 'core' };

describe('concerns', () => {
  const every: Scope[] = [
    { view: 'status' },
    { view: 'overview' },
    { view: 'repositories' },
    { view: 'repository', repository: 'o/a' },
    { view: 'pairs', kind: 'issue' },
    { view: 'pair', id: 7 },
    { view: 'queue' },
    { view: 'people' },
    { view: 'person', login: 'alice', side: 'github' },
  ];

  it('reloads everything on ready and resync', () => {
    for (const scope of every) {
      expect(concerns({ kind: 'ready' }, scope)).toBe(true);
      expect(concerns({ kind: 'resync' }, scope)).toBe(true);
    }
  });

  it('reloads the overview for anything', () => {
    for (const e of [pair(), queue(), cursor(), leader, rateLimit]) {
      expect(concerns(e, { view: 'overview' })).toBe(true);
    }
  });

  it('reloads the status on the leader, the queue and the rate limit only', () => {
    expect(concerns(leader, { view: 'status' })).toBe(true);
    expect(concerns(rateLimit, { view: 'status' })).toBe(true);
    expect(concerns(queue(), { view: 'status' })).toBe(true);
    expect(concerns(pair(), { view: 'status' })).toBe(false);
    expect(concerns(cursor(), { view: 'status' })).toBe(false);
  });

  it('reloads the repositories for pairs, queue and polls, not the leader', () => {
    const scope: Scope = { view: 'repositories' };
    expect(concerns(pair(), scope)).toBe(true);
    expect(concerns(queue(), scope)).toBe(true);
    expect(concerns(cursor(), scope)).toBe(true);
    expect(concerns(leader, scope)).toBe(false);
  });

  it('reloads a repository for its own events, and for pairs that name none', () => {
    const scope: Scope = { view: 'repository', repository: 'o/a' };
    expect(concerns(pair('o/a'), scope)).toBe(true);
    expect(concerns(pair(), scope)).toBe(true);
    expect(concerns(pair('o/b'), scope)).toBe(false);
    expect(concerns(queue('o/b'), scope)).toBe(false);
    expect(concerns(cursor('o/a'), scope)).toBe(true);
    expect(concerns(leader, scope)).toBe(false);
  });

  it('reloads a list for pairs of its kind and repository, and the queue', () => {
    const scope: Scope = { view: 'pairs', kind: 'pull', repository: 'o/a' };
    expect(concerns(pair('o/a', 'pull'), scope)).toBe(true);
    expect(concerns(pair('o/a', 'issue'), scope)).toBe(false);
    expect(concerns(pair('o/b', 'pull'), scope)).toBe(false);
    expect(concerns(pair(), scope)).toBe(true);
    expect(concerns(queue('o/a'), scope)).toBe(true);
    expect(concerns(queue('o/b'), scope)).toBe(false);
    expect(concerns(cursor('o/a'), scope)).toBe(false);
    expect(concerns(queue('o/b'), { view: 'pairs', kind: 'pull' })).toBe(true);
  });

  it('reloads a pair for itself and the queue', () => {
    const scope: Scope = { view: 'pair', id: 7 };
    expect(concerns(pair('o/a', 'issue', 7), scope)).toBe(true);
    expect(concerns(pair('o/a', 'issue', 8), scope)).toBe(false);
    expect(concerns(queue(), scope)).toBe(true);
  });

  it('leaves lists and details alone when the rate limit moves', () => {
    for (const scope of every.filter((s) => s.view !== 'status' && s.view !== 'overview')) {
      expect(concerns(rateLimit, scope)).toBe(false);
    }
  });

  it('reloads the people for pairs, and a person for pairs and the queue', () => {
    expect(concerns(pair(), { view: 'people' })).toBe(true);
    expect(concerns(queue(), { view: 'people' })).toBe(false);
    expect(concerns(cursor(), { view: 'people' })).toBe(false);
    const person: Scope = { view: 'person', login: 'alice', side: 'github' };
    expect(concerns(pair(), person)).toBe(true);
    expect(concerns(queue(), person)).toBe(true);
    expect(concerns(leader, person)).toBe(false);
  });

  it('reloads the queue for the queue only', () => {
    expect(concerns(queue(), { view: 'queue' })).toBe(true);
    expect(concerns(pair(), { view: 'queue' })).toBe(false);
    expect(concerns(cursor(), { view: 'queue' })).toBe(false);
  });
});

describe('parseEvent', () => {
  it('reads an event and its data', () => {
    expect(parseEvent('pair', '{"op":"insert","id":3,"pairKind":"pull"}')).toEqual({
      kind: 'pair',
      op: 'insert',
      id: 3,
      pairKind: 'pull',
    });
    expect(parseEvent('ready', '')).toEqual({ kind: 'ready' });
  });

  it('drops what it does not know or cannot read', () => {
    expect(parseEvent('message', '{}')).toBeNull();
    expect(parseEvent('pair', '{not json')).toBeNull();
  });
});

describe('debounce', () => {
  it('runs once after a burst', () => {
    vi.useFakeTimers();
    let n = 0;
    const f = debounce(() => n++, 400);
    f();
    f();
    vi.advanceTimersByTime(399);
    f();
    expect(n).toBe(0);
    vi.advanceTimersByTime(400);
    expect(n).toBe(1);
    vi.useRealTimers();
  });
});
