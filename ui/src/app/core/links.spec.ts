import { pairsLink, queueLink, repositoryLink } from './links';

describe('links', () => {
  it('builds filtered lists, leaving out what is not set', () => {
    expect(pairsLink('issue', { state: 'closed', repository: 'o/a' })).toEqual({
      commands: ['/issues'],
      query: { state: 'closed', repository: 'o/a' },
    });
    expect(pairsLink('pull', { state: 'any', author: 'alice', origin: 'github' })).toEqual({
      commands: ['/pulls'],
      query: { state: 'any', author: 'alice', origin: 'github' },
    });
    expect(pairsLink('pull', { state: 'merged', repository: '' }).query).toEqual({
      state: 'merged',
    });
  });

  it('builds the queue, failing only when asked', () => {
    expect(queueLink()).toEqual({ commands: ['/queue'], query: {} });
    expect(queueLink({ repository: 'o/a', failing: true }).query).toEqual({
      repository: 'o/a',
      failing: true,
    });
    expect(queueLink({ failing: false }).query).toEqual({});
  });

  it('builds a repository page', () => {
    expect(repositoryLink('metapatrol/foo').commands).toEqual([
      '/repositories',
      'metapatrol',
      'foo',
    ]);
  });
});
