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
  | { kind: 'comment'; at: string; origin: Side; links: HistoryLink[] }
  | { kind: 'import'; at: string }
  | { kind: 'change'; at: string }
  | { kind: 'queued'; at: string };

const SIDE: Record<Side, string> = { github: 'GitHub', forgejo: 'Forgejo' };

export function sideName(side: Side): string {
  return SIDE[side];
}

/**
 * A pair's history, oldest first: when it was paired, each comment the
 * bridge carried across, the pull request's import state, the last change,
 * and what is queued for it, which always comes last. The bridge records
 * no more than this: no comment texts, no comment authors, no earlier
 * states.
 */
export function history(p: PairDetail): HistoryEntry[] {
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
      links: c.origin === 'github' ? links : links.reverse(),
    });
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
