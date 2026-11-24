import type { Snapshot, SnapshotPr, SnapshotTicket } from '@/api/client';

export const NOW = Date.parse('2026-11-24T12:00:00Z');

export function ticket(key: string, over: Partial<SnapshotTicket> = {}): SnapshotTicket {
  return {
    key,
    source: 'branch',
    found: true,
    summary: `Summary of ${key}`,
    status: 'In Review',
    statusCategory: 'indeterminate',
    project: key.split('-')[0] ?? 'INP',
    issueType: 'Story',
    url: `https://acme.atlassian.net/browse/${key}`,
    waiting: null,
    mergePlan: null,
    ...over,
  };
}

export function pr(id: string, number: number, over: Partial<SnapshotPr> = {}): SnapshotPr {
  return {
    id,
    number,
    title: `Change ${number}`,
    repo: {
      id: 'R1',
      nameWithOwner: 'acme/web',
      isNew: false,
      squashAllowed: true,
      mergeAllowed: true,
      rebaseAllowed: true,
    },
    isDraft: false,
    mergeable: 'MERGEABLE',
    mergeStateStatus: 'CLEAN',
    reviewDecision: 'APPROVED',
    headRef: `feat/${number}`,
    headSha: 'abc',
    baseRef: 'main',
    committedAt: '2026-11-24T10:00:00Z',
    ci: { state: 'passing', total: 0, checks: [] },
    reviews: [],
    reviewRequests: [],
    stack: null,
    tickets: [],
    removedKeys: [],
    ticketLookup: 'ok',
    mergeMethod: {
      method: 'squash',
      default: 'squash',
      source: 'repo default',
      allowed: ['squash', 'merge', 'rebase'],
      warning: null,
    },
    ...over,
  };
}

export function snapshot(prs: SnapshotPr[], over: Partial<Snapshot> = {}): Snapshot {
  return {
    version: 1,
    generatedAt: '2026-11-24T12:00:00Z',
    sources: {
      github: { state: 'ok', syncedAt: '2026-11-24T11:59:00Z', error: null, resetAt: null },
      jira: { state: 'ok', syncedAt: '2026-11-24T11:59:00Z', error: null, resetAt: null },
      blockedOrgs: [],
    },
    prs,
    warnings: [],
    ...over,
  };
}
