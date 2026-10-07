import { ActivityEntry } from '../api/models/activity-entry';
import { activitySentence, pairNoun } from './activity';

const entry = (over: Partial<ActivityEntry>): ActivityEntry => ({
  id: 1,
  at: '2026-10-07T00:00:00Z',
  repository: 'metapatrol/foo',
  side: 'github',
  kind: 'issue',
  action: 'create',
  githubNumber: 3,
  forgejoNumber: 113,
  ...over,
});

describe('activitySentence', () => {
  it('names what was written where, on the side it happened', () => {
    expect(activitySentence(entry({}))).toBe('created GitHub #3');
    expect(activitySentence(entry({ side: 'forgejo', kind: 'comment' }))).toBe(
      'commented on Forgejo #113',
    );
    expect(activitySentence(entry({ kind: 'line comment' }))).toBe(
      'commented on a line of GitHub #3',
    );
    expect(activitySentence(entry({ kind: 'notice' }))).toBe('posted a notice on GitHub #3');
  });

  it('tells edits and deletions of comments from those of the issue', () => {
    expect(activitySentence(entry({ action: 'edit' }))).toBe('edited GitHub #3');
    expect(activitySentence(entry({ kind: 'comment', action: 'edit' }))).toBe(
      'edited a comment on GitHub #3',
    );
    expect(activitySentence(entry({ kind: 'comment', action: 'delete' }))).toBe(
      'deleted a comment on GitHub #3',
    );
  });

  it('says what happened to a pull request', () => {
    expect(activitySentence(entry({ kind: 'pull', action: 'close' }))).toBe('closed GitHub #3');
    expect(activitySentence(entry({ kind: 'pull', action: 'reopen', side: 'forgejo' }))).toBe(
      'reopened Forgejo #113',
    );
    expect(activitySentence(entry({ kind: 'pull', action: 'push', side: 'forgejo' }))).toBe(
      'pushed to pr/3',
    );
    expect(
      activitySentence(entry({ kind: 'pull', action: 'rebase-target', side: 'forgejo' })),
    ).toBe('changed the base of Forgejo #113');
    expect(activitySentence(entry({ kind: 'pull', action: 'touches-ci', side: 'forgejo' }))).toBe(
      'flagged Forgejo #113 as touching CI',
    );
  });

  it('does without a number, and says something for what it does not know', () => {
    expect(activitySentence(entry({ githubNumber: undefined }))).toBe('created GitHub');
    expect(activitySentence(entry({ action: 'lock' }))).toBe('lock issue on GitHub #3');
  });

  it('names the pair after the sentence', () => {
    expect(pairNoun(entry({ pairKind: 'pull' }))).toBe('pull request');
    expect(pairNoun(entry({ pairKind: undefined }))).toBe('');
  });
});
