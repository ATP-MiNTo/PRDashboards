import type { Snapshot, SnapshotPr } from '@/api/client';
import { sourceLabel } from './snapshot';

export interface RefreshToast {
  message: string;
  kind: 'info' | 'success' | 'error';
}

/** What makes two states of a pull request differ for the person looking at the list. */
function signature(pr: SnapshotPr): string {
  return [
    pr.title,
    pr.headSha,
    pr.isDraft,
    pr.mergeable,
    pr.reviewDecision,
    pr.ci.state,
    pr.ci.total,
    pr.tickets.map((t) => `${t.key}:${t.status}`).join(','),
  ].join('|');
}

/** How many pull requests were added, removed or changed between two snapshots. */
export function changedCount(before: Snapshot, after: Snapshot): number {
  const old = new Map(before.prs.map((p) => [p.id, signature(p)]));
  let n = 0;
  for (const p of after.prs) {
    if (old.get(p.id) !== signature(p)) n++;
    old.delete(p.id);
  }
  return n + old.size;
}

const hhmm = (iso: string): string => {
  const d = new Date(iso);
  const two = (n: number) => String(n).padStart(2, '0');
  return `${two(d.getHours())}:${two(d.getMinutes())}`;
};

/** The toast after a refresh that the person asked for: what changed, or which source is failing. */
export function refreshToast(before: Snapshot, after: Snapshot, now: number): RefreshToast {
  const { github, jira } = after.sources;
  if (github.state !== 'ok') {
    return { message: `Refreshed. GitHub ${sourceLabel(github, now)}.`, kind: 'error' };
  }
  if (jira.state === 'unreachable' || jira.state === 'error' || jira.state === 'rate_limited') {
    return { message: `Refreshed. Jira ${sourceLabel(jira, now)}.`, kind: 'error' };
  }
  const n = changedCount(before, after);
  if (n === 0) {
    return {
      message: `Refreshed. Nothing changed since ${hhmm(before.generatedAt)}.`,
      kind: 'info',
    };
  }
  return {
    message: `Refreshed. ${n} pull request${n === 1 ? '' : 's'} changed.`,
    kind: 'success',
  };
}
