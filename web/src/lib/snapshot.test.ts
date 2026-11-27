import { describe, expect, it } from 'vitest';
import type { SnapshotPr } from '@/api/client';
import { NOW, pr, snapshot } from '@/test-fixtures';
import {
  approvalsBlock,
  blockOf,
  checkDuration,
  countdown,
  githubChecksUrl,
  githubPrUrl,
  groupsOf,
  isStale,
  matches,
  STALE_AFTER_MS,
  orgSsoUrl,
  readOnlyReason,
  readyBlockOf,
  relativeTime,
  safeLink,
} from './snapshot';

describe('links', () => {
  it('builds GitHub URLs from validated parts only', () => {
    expect(githubPrUrl('acme/web', 7)).toBe('https://github.com/acme/web/pull/7');
    expect(githubChecksUrl('acme/web', 7)).toBe('https://github.com/acme/web/pull/7/checks');
    expect(githubPrUrl('acme/web/extra', 7)).toBeNull();
    expect(githubPrUrl('acme/we b', 7)).toBeNull();
    expect(githubPrUrl('../web', 7)).toBeNull();
    expect(githubPrUrl('acme/web', 0)).toBeNull();
    expect(orgSsoUrl('acme-corp')).toBe('https://github.com/orgs/acme-corp/sso');
    expect(orgSsoUrl('evil/../x')).toBeNull();
    expect(orgSsoUrl('')).toBeNull();
  });

  it('accepts https links only, and shows the host of external ones', () => {
    expect(safeLink(null)).toBeNull();
    expect(safeLink('not a url')).toBeNull();
    expect(safeLink('http://ci.example.com/1')).toBeNull();
    expect(safeLink('javascript:alert(1)')).toBeNull();
    expect(safeLink('https://user:pw@ci.example.com/1')).toBeNull();
    expect(safeLink('https://ci.example.com/job/1?x=1')).toEqual({
      href: 'https://ci.example.com/job/1?x=1',
      host: 'ci.example.com',
    });
    expect(safeLink('https://github.com/acme/web/actions/runs/1?check=1#x')).toEqual({
      href: 'https://github.com/acme/web/actions/runs/1',
      host: null,
    });
    expect(safeLink('https://github.com/a/%2e%2e/b')?.href).toBe('https://github.com/b');
    expect(safeLink('https://github.com/a b/%')).toBeNull();
  });
});

describe('approvalsBlock', () => {
  it('says what is missing, as the server does', () => {
    expect(approvalsBlock({ have: 1, need: 2, changesRequested: 0 })).toBe(
      'Needs 2 approvals, has 1',
    );
    expect(approvalsBlock({ have: 2, need: 2, changesRequested: 0 })).toBeNull();
  });

  it('never blocks a repository that requires no approval', () => {
    expect(approvalsBlock({ have: 0, need: 0, changesRequested: 0 })).toBeNull();
    expect(approvalsBlock({ have: 0, need: 0, changesRequested: 1 })).toBeNull();
    expect(approvalsBlock({ have: 0, need: 1, changesRequested: 0 })).toBe(
      'Needs 1 approvals, has 0',
    );
  });

  it('treats a need outside 0 to 10 as the default of none', () => {
    expect(approvalsBlock({ have: 0, need: 11, changesRequested: 0 })).toBeNull();
    expect(approvalsBlock({ have: 0, need: -1, changesRequested: 0 })).toBeNull();
  });
});

describe('blockOf', () => {
  it.each([
    ['draft', { isDraft: true }, 'draft', 'Draft'],
    ['conflicts', { mergeable: 'CONFLICTING' as const }, 'blocked', 'Merge conflicts'],
    [
      'failing CI',
      { ci: { state: 'failing' as const, total: 0, checks: [] } },
      'blocked',
      'CI failing',
    ],
    [
      'running CI',
      { ci: { state: 'running' as const, total: 0, checks: [] } },
      'blocked',
      'CI running',
    ],
    ['changes', { reviewDecision: 'CHANGES_REQUESTED' as const }, 'blocked', 'Changes requested'],
    [
      'too few approvals',
      { approvals: { have: 1, need: 2, changesRequested: 0 } },
      'blocked',
      'Needs 2 approvals, has 1',
    ],
    [
      'no approval at all',
      { approvals: { have: 0, need: 1, changesRequested: 0 } },
      'blocked',
      'Needs 1 approvals, has 0',
    ],
    ['enough approvals', { approvals: { have: 3, need: 2, changesRequested: 0 } }, 'ready', null],
    ['review', { reviewDecision: 'REVIEW_REQUIRED' as const }, 'blocked', 'Review required'],
    ['unknown', { mergeable: 'UNKNOWN' as const }, 'blocked', 'Checking mergeability'],
    ['ready', {}, 'ready', null],
  ])('%s', (_n, over, readiness, reason) => {
    expect(blockOf(pr('a', 1, over))).toEqual({ readiness, reason });
  });

  it.each(['READ', 'TRIAGE'] as const)('blocks a repository the account can only %s', (role) => {
    const row = pr('a', 1, { repo: { ...pr('a', 1).repo, viewerPermission: role } });
    expect(blockOf(row)).toEqual({
      readiness: 'blocked',
      reason: "Read-only here: your token's account can't write to acme/web",
    });
  });

  it.each([['WRITE'], ['MAINTAIN'], ['ADMIN'], [undefined]] as const)(
    'blocks nothing for the role %s (a snapshot without the role gates nothing)',
    (role) => {
      const row = pr('a', 1, { repo: { ...pr('a', 1).repo, viewerPermission: role } });
      expect(blockOf(row)).toEqual({ readiness: 'ready', reason: null });
    },
  );

  it('keeps a draft a draft whatever the role, and says read-only before other blocks', () => {
    const repo = { ...pr('a', 1).repo, viewerPermission: 'READ' as const };
    expect(blockOf(pr('a', 1, { repo, isDraft: true }))).toEqual({
      readiness: 'draft',
      reason: 'Draft',
    });
    expect(blockOf(pr('a', 1, { repo, mergeable: 'CONFLICTING' })).reason).toMatch(
      /^Read-only here/,
    );
  });

  it('tells a stacked child to merge after its parent', () => {
    const child = pr('b', 2, {
      stack: { id: 'a', position: 2, size: 2, parentId: 'a', parentNumber: 1 },
    });
    expect(blockOf(child).reason).toBe('Merge after #1');
  });
});

describe('readOnlyReason and readyBlockOf', () => {
  const readOnly = (over: Partial<SnapshotPr> = {}) =>
    pr('a', 1, {
      isDraft: true,
      author: 'priya',
      roles: ['assigned'],
      repo: { ...pr('a', 1).repo, viewerPermission: 'READ' },
      ...over,
    });

  it('names the repository, and is null without a read-only role', () => {
    expect(readOnlyReason(readOnly())).toBe(
      "Read-only here: your token's account can't write to acme/web",
    );
    expect(readOnlyReason(pr('a', 1))).toBeNull();
  });

  it('blocks Ready for a draft of someone else, but not for its author', () => {
    expect(readyBlockOf(readOnly(), 'octocat')).toMatch(/^Read-only here/);
    expect(readyBlockOf(readOnly(), undefined)).toMatch(/^Read-only here/);
    expect(readyBlockOf(readOnly({ author: 'OctoCat' }), 'octocat')).toBeNull();
    expect(readyBlockOf(readOnly({ roles: ['authored'] }), undefined)).toBeNull();
    expect(readyBlockOf(pr('a', 1, { isDraft: true }), 'octocat')).toBeNull();
  });
});

describe('filters and groups', () => {
  const rows = [
    pr('a', 1, { stack: { id: 'a', position: 1, size: 2, parentId: null, parentNumber: null } }),
    pr('c', 3, { ci: { state: 'running', total: 1, checks: [] } }),
    pr('b', 2, { stack: { id: 'a', position: 2, size: 2, parentId: 'a', parentNumber: 1 } }),
    pr('d', 4, { isDraft: true, committedAt: '2026-11-01T00:00:00Z' }),
  ];
  const snap = snapshot(rows);

  it.each([
    ['all', ['a', 'c', 'b', 'd']],
    ['ready', ['a']],
    ['running', ['c']],
    ['blocked', ['c', 'b']],
    ['draft', ['d']],
    ['stale', ['d']],
    ['stacks', ['a', 'b']],
  ] as const)('the %s filter matches %j', (filter, ids) => {
    expect(rows.filter((p) => matches(p, filter, NOW)).map((p) => p.id)).toEqual(ids);
  });

  it('says a pull request is stale after three days without a commit, by the clock it is given', () => {
    const p = pr('x', 1, { committedAt: '2026-11-20T00:00:00Z' });
    const at = Date.parse('2026-11-20T00:00:00Z');
    expect(isStale(p, at + STALE_AFTER_MS)).toBe(false);
    expect(isStale(p, at + STALE_AFTER_MS + 1)).toBe(true);
    expect(isStale(pr('y', 2, { committedAt: null }), at + 99 * STALE_AFTER_MS)).toBe(false);
  });

  it('keeps stacks together in stack order and the rest in list order', () => {
    expect(groupsOf(snap, 'all')).toEqual([
      { key: 'a', ids: ['a', 'b'], stacked: true },
      { key: 'c', ids: ['c'], stacked: false },
      { key: 'd', ids: ['d'], stacked: false },
    ]);
    expect(groupsOf(snap, 'running')).toEqual([{ key: 'c', ids: ['c'], stacked: false }]);
    expect(groupsOf(snap, 'stacks')).toEqual([{ key: 'a', ids: ['a', 'b'], stacked: true }]);
    expect(groupsOf(snap, 'ready')).toEqual([{ key: 'a', ids: ['a'], stacked: true }]);
    expect(groupsOf(snap, 'stale', 'all', [], new Set(), NOW)).toEqual([
      { key: 'd', ids: ['d'], stacked: false },
    ]);
  });

  it('drops the pull requests of a repository that is switched off', () => {
    const repo = snap.prs[0]?.repo.id ?? '';
    expect(groupsOf(snap, 'all', 'all', [], new Set([repo]))).toEqual([]);
    expect(groupsOf(snap, 'all', 'all', [], new Set(['other']))).toHaveLength(3);
  });
});

describe('times', () => {
  it('formats relative times', () => {
    const ago = (s: number) => new Date(NOW - s * 1000).toISOString();
    expect(relativeTime(null, NOW)).toBe('never');
    expect(relativeTime('nope', NOW)).toBe('unknown');
    expect(relativeTime(ago(10), NOW)).toBe('just now');
    expect(relativeTime(ago(-60), NOW)).toBe('just now');
    expect(relativeTime(ago(5 * 60), NOW)).toBe('5 min ago');
    expect(relativeTime(ago(3 * 3600), NOW)).toBe('3 h ago');
    expect(relativeTime(ago(2 * 86400), NOW)).toBe('2 d ago');
  });

  it('counts down', () => {
    expect(countdown(null, NOW)).toBeNull();
    expect(countdown(new Date(NOW + 95_000).toISOString(), NOW)).toBe('1m 35s');
    expect(countdown(new Date(NOW + 9_000).toISOString(), NOW)).toBe('9s');
    expect(countdown(new Date(NOW - 9_000).toISOString(), NOW)).toBe('0s');
  });

  it('measures check durations', () => {
    const base = { name: 'x', app: null, url: null, state: 'passing' as const };
    const t = (s: number) => new Date(NOW + s * 1000).toISOString();
    expect(checkDuration({ ...base, startedAt: null, completedAt: null }, NOW)).toBeNull();
    expect(checkDuration({ ...base, startedAt: t(0), completedAt: t(42) }, NOW)).toBe('42s');
    expect(checkDuration({ ...base, startedAt: t(0), completedAt: t(150) }, NOW)).toBe('2m 30s');
    expect(checkDuration({ ...base, startedAt: t(0), completedAt: t(3900) }, NOW)).toBe('1h 5m');
    expect(
      checkDuration({ ...base, state: 'running', startedAt: t(-30), completedAt: null }, NOW),
    ).toBe('30s');
    expect(checkDuration({ ...base, startedAt: t(0), completedAt: null }, NOW)).toBeNull();
  });
});
