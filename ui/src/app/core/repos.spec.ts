import { Repository } from '../api/models/repository';
import { displayNames, lastPoll, repoHealth } from './repos';

function repo(key: string, depth: number, failing: number, cursors: string[] = []): Repository {
  return {
    key,
    github: key,
    forgejo: key,
    githubUrl: '',
    forgejoUrl: '',
    pullRequests: false,
    targetBranches: [],
    rebaseRequests: false,
    importLabel: 'github',
    publishLabel: 'public',
    counts: { issuesOpen: 0, issuesClosed: 0, pullsOpen: 0, pullsMerged: 0, pullsClosed: 0 },
    queue: { depth, failing },
    cursors: cursors.map((updatedAt) => ({
      side: 'github',
      stream: 'issues',
      cursor: updatedAt,
      updatedAt,
    })),
  };
}

describe('repos', () => {
  it('says what the queue holds', () => {
    expect(repoHealth(repo('o/x', 3, 2))).toEqual({ rank: 0, status: 'error', text: '2 failing' });
    expect(repoHealth(repo('o/x', 2, 0)).text).toBe('2 queued');
    expect(repoHealth(repo('o/x', 0, 0)).text).toBe('in step');
  });

  it('takes the poll that moved last', () => {
    expect(lastPoll(repo('o/x', 0, 0, ['2026-10-06T10:00:00Z', '2026-10-06T11:00:00Z']))).toBe(
      '2026-10-06T11:00:00Z',
    );
    expect(lastPoll(repo('o/x', 0, 0))).toBeUndefined();
  });
});

describe('displayNames', () => {
  it('drops the owner when every repository has the same one', () => {
    const repos = [repo('metapatrol/a', 0, 0), repo('metapatrol/b', 0, 0)];
    expect(repos.map(displayNames(repos))).toEqual(['a', 'b']);
  });

  it('keeps it when owners differ', () => {
    const repos = [repo('metapatrol/a', 0, 0), repo('other/b', 0, 0)];
    expect(repos.map(displayNames(repos))).toEqual(['metapatrol/a', 'other/b']);
  });
});
