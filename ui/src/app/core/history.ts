import { ActivityEntry } from '../api/models/activity-entry';
import { PairDetail } from '../api/models/pair-detail';
import { Side } from '../api/models/side';

/** A link on a history entry. */
export interface HistoryLink {
  label: string;
  href: string;
}

/** One line of a pair's history, built only from what the bridge holds. */
export type HistoryEntry =
  | {
      kind: 'paired';
      at: string;
      origin: Side;
      author: string;
      /** The number on the side it was opened, and on the side it was carried to. */
      from: HistoryLink;
      to: HistoryLink;
    }
  | {
      kind: 'comment';
      at: string;
      origin: Side;
      /** A comment on a line of the diff rather than in the conversation. */
      line: boolean;
      links: HistoryLink[];
    }
  | { kind: 'import'; at: string }
  /** Something else the bridge wrote for the pair: an edit, a close, a push, a notice. */
  | { kind: 'activity'; at: string; entry: ActivityEntry }
  | { kind: 'change'; at: string }
  | { kind: 'queued'; at: string };

const SIDE: Record<Side, string> = { github: 'GitHub', forgejo: 'Forgejo' };

export function sideName(side: Side): string {
  return SIDE[side];
}

/**
 * Activity a history already tells: creating the pair's projection is its
 * pairing, and writing a comment is the comment pair it made.
 */
function told(e: ActivityEntry): boolean {
  return (
    e.action === 'create' &&
    (e.kind === 'issue' || e.kind === 'pull' || e.kind === 'comment' || e.kind === 'line comment')
  );
}

/**
 * A pair's history, oldest first: when it was paired, each comment the
 * bridge carried across, what else the bridge wrote for it (its activity:
 * edits, closes, pushes, notices), the pull request's import state, the last
 * change, and what is queued for it, which always comes last. The bridge
 * records no more than this: no comment texts, no comment authors, no
 * earlier states.
 */
export function history(p: PairDetail, activity: readonly ActivityEntry[] = []): HistoryEntry[] {
  const github: HistoryLink = { label: `GitHub #${p.githubNumber}`, href: p.githubUrl };
  const forgejo: HistoryLink = { label: `Forgejo #${p.forgejoNumber}`, href: p.forgejoUrl };
  const entries: HistoryEntry[] = [
    {
      kind: 'paired',
      at: p.createdAt,
      origin: p.origin,
      author: p.author,
      from: p.origin === 'github' ? github : forgejo,
      to: p.origin === 'github' ? forgejo : github,
    },
  ];
  for (const c of p.commentPairs) {
    const links = [
      { label: 'GitHub', href: c.githubUrl },
      { label: 'Forgejo', href: c.forgejoUrl },
    ];
    entries.push({
      kind: 'comment',
      at: c.createdAt,
      origin: c.origin,
      // Carried onto its line, or into the conversation where GitHub would
      // not place it: a line comment either way.
      line: c.kind !== 'comment',
      links: c.origin === 'github' ? links : links.reverse(),
    });
  }
  for (const e of activity) {
    if (!told(e)) {
      entries.push({ kind: 'activity', at: e.at, entry: e });
    }
  }
  if (p.pull) {
    entries.push({ kind: 'import', at: p.pull.updatedAt });
  }
  // The last change is worth its own line only when nothing above says it.
  if (!entries.some((e) => e.at === p.updatedAt)) {
    entries.push({ kind: 'change', at: p.updatedAt });
  }
  entries.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  if (p.queued) {
    entries.push({ kind: 'queued', at: p.queued.markedAt });
  }
  return entries;
}
