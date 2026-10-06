import { PairDetail } from '../api/models/pair-detail';
import { history } from './history';

const base: PairDetail = {
  id: 7,
  repository: 'metapatrol/foo',
  kind: 'pull',
  origin: 'github',
  state: 'merged',
  title: 'Fix the parser',
  author: 'alice',
  githubNumber: 13,
  forgejoNumber: 113,
  githubUrl: 'https://github.com/metapatrol/foo/pull/13',
  forgejoUrl: 'https://forgejo.example.com/platform/foo/pulls/113',
  comments: 2,
  createdAt: '2026-10-01T10:00:00Z',
  updatedAt: '2026-10-03T10:00:00Z',
  pull: {
    contribBranch: 'pr/13',
    importedHeadSha: '3a5f1c0aa',
    headFrozen: false,
    touchesCi: false,
    rebaseRequested: false,
    outcomeReported: true,
    updatedAt: '2026-10-02T12:00:00Z',
  },
  commentPairs: [
    {
      id: 2,
      origin: 'forgejo',
      githubCommentId: 20,
      forgejoCommentId: 200,
      githubUrl: 'https://github.com/c/20',
      forgejoUrl: 'https://forgejo.example.com/c/200',
      createdAt: '2026-10-02T10:00:00Z',
    },
    {
      id: 1,
      origin: 'github',
      githubCommentId: 10,
      forgejoCommentId: 100,
      githubUrl: 'https://github.com/c/10',
      forgejoUrl: 'https://forgejo.example.com/c/100',
      createdAt: '2026-10-01T11:00:00Z',
    },
  ],
};

describe('history', () => {
  it('runs oldest first: paired, comments, import state, last change', () => {
    expect(history(base).map((e) => `${e.kind}@${e.at.slice(5, 13)}`)).toEqual([
      'paired@10-01T10',
      'comment@10-01T11',
      'comment@10-02T10',
      'import@10-02T12',
      'change@10-03T10',
    ]);
  });

  it('says where it came from and where it went', () => {
    const [paired] = history(base);
    expect(paired).toMatchObject({
      origin: 'github',
      author: 'alice',
      from: { label: 'GitHub #13' },
      to: { label: 'Forgejo #113' },
    });
    const published = history({ ...base, kind: 'issue', origin: 'forgejo', pull: undefined })[0];
    expect(published).toMatchObject({
      from: { label: 'Forgejo #113' },
      to: { label: 'GitHub #13' },
    });
  });

  it('links a comment from the side it was written on', () => {
    const comments = history(base).filter((e) => e.kind === 'comment');
    expect(comments.map((c) => (c.kind === 'comment' ? c.links[0].label : ''))).toEqual([
      'GitHub',
      'Forgejo',
    ]);
  });

  it('has no import state for an issue, and no extra last change when nothing came later', () => {
    const issue = history({
      ...base,
      kind: 'issue',
      pull: undefined,
      commentPairs: [],
      updatedAt: base.createdAt,
    });
    expect(issue.map((e) => e.kind)).toEqual(['paired']);
  });

  it('puts what is queued last', () => {
    const entries = history({
      ...base,
      queued: {
        repository: 'metapatrol/foo',
        side: 'github',
        number: 13,
        url: '',
        markedAt: '2026-09-01T00:00:00Z',
        notBefore: '2026-10-04T00:00:00Z',
        attempts: 3,
        lastError: 'boom',
      },
    });
    expect(entries.at(-1)?.kind).toBe('queued');
  });
});
