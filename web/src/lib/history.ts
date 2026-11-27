import type { MergeResult, MergeStep, SnapshotPr } from '@/api/client';
import { githubPrUrl } from './snapshot';

/** What became of one Jira ticket of a pull request that was merged. */
export interface HistoryTicket {
  key: string;
  /** moved: it went to `status`. left: it stayed in `status`. failed: the move did not work. queued: GitHub has not merged yet. */
  kind: 'moved' | 'left' | 'failed' | 'queued';
  status: string | null;
  /** The ticket in Jira, from the snapshot, when it had one. */
  url: string | null;
  /** The pending retry of a failed move. */
  retryId: number | null;
}

/** One merge made from the dashboard, as the History view lists it. */
export interface HistoryEntry {
  prId: string;
  title: string;
  repo: string;
  number: number;
  author: string | null;
  additions: number | null;
  deletions: number | null;
  /** GitHub only added it to the merge queue. */
  queued: boolean;
  /** When the server answered the merge, in milliseconds. */
  at: number;
  tickets: HistoryTicket[];
  /** Whether a step other than a ticket move failed, such as deleting the branch. */
  otherFailed: boolean;
  /** What the server said about each failed step, one line each ("INP-104: Jira rejected the move"). */
  problems: string[];
}

const stepOf = (steps: readonly MergeStep[], key: string) =>
  steps.find((s) => s.kind === 'transition' && s.ticket === key);

/** The History entry of a merge, from the pull request as it was when it merged and the server's steps. */
export function historyEntry(pr: SnapshotPr, result: MergeResult, at: number): HistoryEntry {
  const { steps } = result;
  const keys = [
    ...pr.tickets.map((t) => t.key),
    ...steps.flatMap((s) => (s.kind === 'transition' && s.ticket ? [s.ticket] : [])),
  ].filter((k, i, all) => all.indexOf(k) === i);
  const tickets = keys.map((key): HistoryTicket => {
    const t = pr.tickets.find((x) => x.key === key);
    const step = stepOf(steps, key);
    const base = { key, url: t?.url ?? null, retryId: null };
    if (step?.state === 'ok' && step.toStatus) {
      return { ...base, kind: 'moved', status: step.toStatus };
    }
    if (step?.state === 'failed')
      return { ...base, kind: 'failed', status: null, retryId: step.retryId };
    if (step?.state === 'queued') return { ...base, kind: 'queued', status: step.toStatus };
    return { ...base, kind: 'left', status: t?.status ?? null };
  });
  const merge = steps.find((s) => s.kind === 'merge');
  return {
    prId: pr.id,
    title: pr.title,
    repo: pr.repo.nameWithOwner,
    number: pr.number,
    author: pr.author,
    additions: pr.additions,
    deletions: pr.deletions,
    queued: merge?.state === 'queued',
    at,
    tickets,
    otherFailed: steps.some((s) => s.kind !== 'transition' && s.state === 'failed'),
    problems: steps.flatMap((s) => {
      if (s.state !== 'failed' || !s.message) return [];
      return [s.kind === 'transition' && s.ticket ? `${s.ticket}: ${s.message}` : s.message];
    }),
  };
}

/** The words of a ticket badge: "INP-104 moved to Done", "INP-105 left in In Review". */
export function ticketText(t: HistoryTicket): string {
  switch (t.kind) {
    case 'moved':
      return `${t.key} moved to ${t.status}`;
    case 'queued':
      return t.status ? `${t.key} will move to ${t.status}` : `${t.key} will move`;
    case 'failed':
      return `${t.key} did not move`;
    default:
      return t.status ? `${t.key} left in ${t.status}` : `${t.key} left as it was`;
  }
}

/** Whether an entry answers the header search: every word is in the title, repository, number or a ticket key. */
export function matchesHistory(e: HistoryEntry, terms: readonly string[]): boolean {
  if (terms.length === 0) return true;
  const text = [e.title, e.repo, String(e.number), ...e.tickets.map((t) => t.key)]
    .join('\n')
    .toLowerCase();
  return terms.every((t) => text.includes(t));
}

/** When it merged, in the words of the design: "just now", "5m ago", "2h ago", "Yesterday", "3d ago". */
export function mergedWhen(at: number, now: number): string {
  const min = Math.max(0, Math.floor((now - at) / 60_000));
  if (min < 1) return 'just now';
  if (min < 60) return `${min}m ago`;
  const hours = Math.floor(min / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return days === 1 ? 'Yesterday' : `${days}d ago`;
}

/** The pull request on GitHub, rebuilt from validated parts. */
export const historyUrl = (e: HistoryEntry): string | null => githubPrUrl(e.repo, e.number);
