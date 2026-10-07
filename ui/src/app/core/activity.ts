import { ActivityEntry } from '../api/models/activity-entry';
import { sideName } from './history';

/** What the bridge writes, as the activity filter offers it. */
export const ACTIVITY_KINDS: { value: string; label: string }[] = [
  { value: 'issue', label: 'issues' },
  { value: 'pull', label: 'pull requests' },
  { value: 'comment', label: 'comments' },
  { value: 'line comment', label: 'line comments' },
  { value: 'notice', label: 'notices' },
];

/** What the bridge did, as the activity filter offers it. */
export const ACTIVITY_ACTIONS: { value: string; label: string }[] = [
  { value: 'create', label: 'created' },
  { value: 'edit', label: 'edited' },
  { value: 'close', label: 'closed' },
  { value: 'reopen', label: 'reopened' },
  { value: 'delete', label: 'deleted' },
  { value: 'push', label: 'pushed' },
  { value: 'rebase-target', label: 'retargeted' },
  { value: 'touches-ci', label: 'flagged as touching CI' },
];

/** The columns the activity can be sorted by. */
export const ACTIVITY_SORTS = ['at', 'repository', 'kind', 'action', 'side'] as const;

/** What a kind of write is called inside a sentence. */
const NOUN: Record<string, string> = {
  issue: 'issue',
  pull: 'pull request',
  comment: 'comment',
  'line comment': 'line comment',
  notice: 'notice',
};

/** Where it happened: "GitHub #3", or the side alone without a number. */
function where(e: ActivityEntry): string {
  const n = e.side === 'github' ? e.githubNumber : e.forgejoNumber;
  return n ? `${sideName(e.side)} #${n}` : sideName(e.side);
}

/**
 * One write of the bridge as a short sentence, in the bridge's voice:
 * "created GitHub #3", "commented on Forgejo #113", "pushed to pr/13". The
 * subject is always the bridge, so it is left out.
 */
export function activitySentence(e: ActivityEntry): string {
  const at = where(e);
  const noun = NOUN[e.kind] ?? e.kind;
  const onSomething = e.kind === 'comment' || e.kind === 'line comment' || e.kind === 'notice';
  switch (e.action) {
    case 'create':
      if (e.kind === 'comment') return `commented on ${at}`;
      if (e.kind === 'line comment') return `commented on a line of ${at}`;
      if (e.kind === 'notice') return `posted a notice on ${at}`;
      return `created ${at}`;
    case 'edit':
      return onSomething ? `edited a ${noun} on ${at}` : `edited ${at}`;
    case 'delete':
      return onSomething ? `deleted a ${noun} on ${at}` : `deleted ${at}`;
    case 'close':
      return `closed ${at}`;
    case 'reopen':
      return `reopened ${at}`;
    case 'push':
      // Contrib branches are named after the GitHub pull request.
      return e.githubNumber ? `pushed to pr/${e.githubNumber}` : `pushed to ${at}`;
    case 'rebase-target':
      return `changed the base of ${at}`;
    case 'touches-ci':
      return `flagged ${at} as touching CI`;
    default:
      return `${e.action} ${noun} on ${at}`;
  }
}

/** What the pair is, after the sentence: "issue", "pull request". */
export function pairNoun(e: ActivityEntry): string {
  return e.pairKind === 'pull' ? 'pull request' : e.pairKind === 'issue' ? 'issue' : '';
}
