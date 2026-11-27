import { describe, expect, it } from 'vitest';
import { NOW, pr, snapshot } from '@/test-fixtures';
import {
  approvalsBlock,
  blockOf,
  checkDuration,
  countdown,
  githubChecksUrl,
  githubPrUrl,
  groupsOf,
  matches,
  orgSsoUrl,
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

  it('accepts https links on the host of their kind only', () => {
    for (const kind of ['github', 'jira'] as const) {
      expect(safeLink(null, kind)).toBeNull();
      expect(safeLink('not a url', kind)).toBeNull();
      expect(safeLink('javascript:alert(1)', kind)).toBeNull();
      expect(safeLink('http://github.com/a/b', kind)).toBeNull();
      expect(safeLink('http://acme.atlassian.net/browse/A-1', kind)).toBeNull();
      expect(safeLink('https://ci.example.com/job/1?x=1', kind)).toBeNull();
    }
  });

  it('rebuilds a github link from its path', () => {
    expect(safeLink('https://github.com/acme/web/actions/runs/1?check=1#x', 'github')).toEqual({
      href: 'https://github.com/acme/web/actions/runs/1',
      host: null,
    });
    expect(safeLink('https://github.com/a/%2e%2e/b', 'github')?.href).toBe('https://github.com/b');
    expect(safeLink('https://github.com/a b/%', 'github')).toBeNull();
    expect(safeLink('https://user:pw@github.com/a/b', 'github')).toBeNull();
    expect(safeLink('https://github.com:8443/a/b', 'github')).toBeNull();
    expect(safeLink('https://api.github.com/a/b', 'github')).toBeNull();
    expect(safeLink('https://github.com.evil.example/a/b', 'github')).toBeNull();
    expect(safeLink('https://acme.atlassian.net/browse/A-1', 'github')).toBeNull();
  });

  it('accepts a jira link on an atlassian.net site and shows its host', () => {
    expect(safeLink('https://acme.atlassian.net/browse/A-1', 'jira')).toEqual({
      href: 'https://acme.atlassian.net/browse/A-1',
      host: 'acme.atlassian.net',
    });
    expect(safeLink('https://ACME.atlassian.net/browse/A-1', 'jira')?.host).toBe(
      'acme.atlassian.net',
    );
    expect(safeLink('https://atlassian.net/browse/A-1', 'jira')).toBeNull();
    expect(safeLink('https://acme.atlassian.net.evil.example/browse/A-1', 'jira')).toBeNull();
    expect(safeLink('https://evilatlassian.net/browse/A-1', 'jira')).toBeNull();
    expect(safeLink('https://acme.atlassian.net@evil.example/browse/A-1', 'jira')).toBeNull();
    expect(safeLink('https://user:pw@acme.atlassian.net/browse/A-1', 'jira')).toBeNull();
    expect(safeLink('https://acme.atlassian.net:8443/browse/A-1', 'jira')).toBeNull();
    expect(safeLink('https://github.com/acme/web', 'jira')).toBeNull();
  });
});

describe('approvalsBlock', () => {
  it('says what is missing, as the server does', () => {
    expect(approvalsBlock({ have: 1, need: 2, changesRequested: 0 })).toBe(
      'Needs 2 approvals, has 1',
    );
    expect(approvalsBlock({ have: 2, need: 2, changesRequested: 0 })).toBeNull();
  });

  it('treats a need outside 1 to 10 as the default of one', () => {
    expect(approvalsBlock({ have: 0, need: 0, changesRequested: 0 })).toBe(
      'Needs 1 approvals, has 0',
    );
    expect(approvalsBlock({ have: 1, need: 11, changesRequested: 0 })).toBeNull();
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

  it('tells a stacked child to merge after its parent', () => {
    const child = pr('b', 2, {
      stack: { id: 'a', position: 2, size: 2, parentId: 'a', parentNumber: 1 },
    });
    expect(blockOf(child).reason).toBe('Merge after #1');
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
    ['stacks', ['a', 'b']],
  ] as const)('the %s filter matches %j', (filter, ids) => {
    expect(rows.filter((p) => matches(p, filter)).map((p) => p.id)).toEqual(ids);
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
  });

  it('drops the pull requests of a repository that is switched off', () => {
    const repo = snap.prs[0]?.repo.id ?? '';
    expect(groupsOf(snap, 'all', 'everything', [], new Set([repo]))).toEqual([]);
    expect(groupsOf(snap, 'all', 'everything', [], new Set(['other']))).toHaveLength(3);
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
