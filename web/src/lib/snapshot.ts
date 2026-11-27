import type { Approvals, Check, Snapshot, SnapshotPr, SourceStatus } from '@/api/client';
import { matchesSearch } from './search';
import { inShow, type ShowMode } from './show';

const ORG = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$/;
const REPO = /^[A-Za-z0-9._-]{1,100}$/;
const PATH = /^\/[A-Za-z0-9._\-/]*$/;

export function splitRepo(nameWithOwner: string): [string, string] | null {
  const [owner, repo, ...rest] = nameWithOwner.split('/');
  if (rest.length > 0 || !owner || !repo || !ORG.test(owner) || !REPO.test(repo)) return null;
  return [owner, repo];
}

export function githubPrUrl(nameWithOwner: string, number: number): string | null {
  const parts = splitRepo(nameWithOwner);
  if (!parts || !Number.isInteger(number) || number < 1) return null;
  return `https://github.com/${parts[0]}/${parts[1]}/pull/${number}`;
}

/** Where a pull request's checks are listed, for "+N more". */
export function githubChecksUrl(nameWithOwner: string, number: number): string | null {
  const pr = githubPrUrl(nameWithOwner, number);
  return pr && `${pr}/checks`;
}

/** The organization's SSO page, built from a validated name and never from a server URL. */
export function orgSsoUrl(org: string): string | null {
  return ORG.test(org) ? `https://github.com/orgs/${org}/sso` : null;
}

export interface SafeLink {
  href: string;
  /** The host to show for a link that leaves GitHub. */
  host: string | null;
}

/**
 * Validates a link from a provider. Only https without credentials is accepted. A github.com link
 * is rebuilt from its path alone, so no query or fragment is carried over.
 */
export function safeLink(raw: string | null): SafeLink | null {
  if (!raw) return null;
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return null;
  }
  if (u.protocol !== 'https:' || u.username || u.password || !u.hostname) return null;
  if (u.hostname === 'github.com') {
    return PATH.test(u.pathname) && !u.pathname.includes('..')
      ? { href: `https://github.com${u.pathname}`, host: null }
      : null;
  }
  return { href: u.href, host: u.host };
}

export type Readiness = 'ready' | 'blocked' | 'draft';

export interface Block {
  readiness: Readiness;
  /** Why the pull request cannot be merged yet, in the words of the Merge column. */
  reason: string | null;
}

/**
 * Why the approvals alone keep a pull request from merging, or null. The words are the server's
 * (`merge.Approvals.Block` in Go), which refuses the merge with the same text.
 */
export function approvalsBlock(a: Approvals): string | null {
  const need = a.need >= 1 && a.need <= 10 ? a.need : 1;
  return a.have >= need ? null : `Needs ${need} approvals, has ${a.have}`;
}

export function blockOf(pr: SnapshotPr, parentNumber?: number | null): Block {
  if (pr.isDraft) return { readiness: 'draft', reason: 'Draft' };
  const parent = parentNumber ?? pr.stack?.parentNumber ?? null;
  if (pr.stack?.parentId && parent !== null) {
    return { readiness: 'blocked', reason: `Merge after #${parent}` };
  }
  if (pr.mergeable === 'CONFLICTING') return { readiness: 'blocked', reason: 'Merge conflicts' };
  if (pr.ci.state === 'failing') return { readiness: 'blocked', reason: 'CI failing' };
  if (pr.ci.state === 'running') return { readiness: 'blocked', reason: 'CI running' };
  if (pr.reviewDecision === 'CHANGES_REQUESTED') {
    return { readiness: 'blocked', reason: 'Changes requested' };
  }
  const approvals = approvalsBlock(pr.approvals);
  if (approvals) return { readiness: 'blocked', reason: approvals };
  if (pr.reviewDecision === 'REVIEW_REQUIRED') {
    return { readiness: 'blocked', reason: 'Review required' };
  }
  if (pr.mergeable === 'UNKNOWN') return { readiness: 'blocked', reason: 'Checking mergeability' };
  return { readiness: 'ready', reason: null };
}

/** What the stat tiles filter the list by: everything, ready to merge, running CI, or stacks only. */
export type Filter = 'all' | 'ready' | 'running' | 'stacks';

export const FILTERS: Filter[] = ['all', 'ready', 'running', 'stacks'];

export function matches(pr: SnapshotPr, filter: Filter): boolean {
  switch (filter) {
    case 'all':
      return true;
    case 'ready':
      return blockOf(pr).readiness === 'ready';
    case 'running':
      return pr.ci.state === 'running';
    case 'stacks':
      return pr.stack !== null;
  }
}

export interface Group {
  /** The id of the base pull request for a stack, else the pull request itself. */
  key: string;
  ids: string[];
  stacked: boolean;
}

const NO_REPOS: ReadonlySet<string> = new Set();

/**
 * Groups the filtered pull requests into rows: stacks stay together, in stack order, and everything
 * else is its own group. Only pull requests the Show mode lists, the search terms match and a repository
 * that is not switched off (`off`, repository ids) has are kept. The result holds ids only, so a row's data changing leaves it equal.
 */
export function groupsOf(
  s: Snapshot,
  filter: Filter,
  show: ShowMode = 'everything',
  terms: readonly string[] = [],
  off: ReadonlySet<string> = NO_REPOS,
): Group[] {
  const groups: Group[] = [];
  const byStack = new Map<string, Group>();
  const rows = [...s.prs].sort((a, b) => (a.stack?.position ?? 0) - (b.stack?.position ?? 0));
  const kept = new Set(
    rows
      .filter(
        (p) =>
          !off.has(p.repo.id) && matches(p, filter) && inShow(p, show) && matchesSearch(p, terms),
      )
      .map((p) => p.id),
  );
  for (const pr of s.prs) {
    if (!kept.has(pr.id) || pr.stack) continue;
    groups.push({ key: pr.id, ids: [pr.id], stacked: false });
  }
  for (const pr of rows) {
    if (!kept.has(pr.id) || !pr.stack) continue;
    let g = byStack.get(pr.stack.id);
    if (!g) {
      g = { key: pr.stack.id, ids: [], stacked: true };
      byStack.set(pr.stack.id, g);
      groups.push(g);
    }
    g.ids.push(pr.id);
  }
  return groups.sort((a, b) => order(s, a) - order(s, b));
}

function order(s: Snapshot, g: Group): number {
  return s.prs.findIndex((p) => p.id === g.ids[0]);
}

export function relativeTime(iso: string | null, now: number): string {
  if (!iso) return 'never';
  const sec = Math.round((now - Date.parse(iso)) / 1000);
  if (Number.isNaN(sec)) return 'unknown';
  if (sec < 0) return 'just now';
  if (sec < 45) return 'just now';
  const min = Math.round(sec / 60);
  if (min < 60) return `${min} min ago`;
  const h = Math.round(min / 60);
  if (h < 24) return `${h} h ago`;
  const d = Math.round(h / 24);
  return `${d} d ago`;
}

/** Time until a moment, for the next-sync countdown. */
export function countdown(iso: string | null, now: number): string | null {
  if (!iso) return null;
  const sec = Math.max(0, Math.round((Date.parse(iso) - now) / 1000));
  if (Number.isNaN(sec)) return null;
  const m = Math.floor(sec / 60);
  return m > 0 ? `${m}m ${String(sec % 60).padStart(2, '0')}s` : `${sec}s`;
}

export function absoluteTime(iso: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleString();
}

export function checkDuration(c: Check, now: number): string | null {
  if (!c.startedAt) return null;
  const start = Date.parse(c.startedAt);
  const end = c.completedAt ? Date.parse(c.completedAt) : c.state === 'running' ? now : NaN;
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return null;
  const sec = Math.round((end - start) / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  return min < 60 ? `${min}m ${sec % 60}s` : `${Math.floor(min / 60)}h ${min % 60}m`;
}

const SOURCE_TEXT: Record<SourceStatus['state'], string> = {
  pending: 'waiting',
  ok: 'ok',
  rate_limited: 'rate limited',
  error: 'error',
  unreachable: 'unreachable',
  disconnected: 'not connected',
};

/** The short state of a source for the top bar. A rate limit shows when it resets. */
export function sourceLabel(s: SourceStatus, now: number): string {
  const base = SOURCE_TEXT[s.state];
  if (s.state !== 'rate_limited') return base;
  const left = countdown(s.resetAt, now);
  return left ? `${base}, resets in ${left}` : base;
}

/** Set while GitHub fails, so every row can say its data is the last good copy and how old it is. */
export function lastGoodGithub(s: Snapshot): { syncedAt: string | null } | null {
  const g = s.sources.github;
  return g.state === 'rate_limited' || g.state === 'error' ? { syncedAt: g.syncedAt } : null;
}
