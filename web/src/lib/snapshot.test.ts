import { describe, expect, it } from 'vitest';
import { NOW, pr, snapshot } from '@/test-fixtures';
import {
  blockOf,
  checkDuration,
  countdown,
  githubChecksUrl,
  githubPrUrl,
  groupsOf,
  isStale,
  matches,
  orgSsoUrl,
  relativeTime,
  safeLink,
  statsOf,
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

describe('stats and groups', () => {
  const rows = [
    pr('a', 1, { stack: { id: 'a', position: 1, size: 2, parentId: null, parentNumber: null } }),
    pr('c', 3),
    pr('b', 2, { stack: { id: 'a', position: 2, size: 2, parentId: 'a', parentNumber: 1 } }),
    pr('d', 4, { isDraft: true, committedAt: '2026-11-01T00:00:00Z' }),
  ];
  const snap = snapshot(rows);

  it('counts', () => {
    expect(statsOf(snap, NOW)).toEqual({ open: 4, ready: 2, blocked: 1, draft: 1, stale: 1 });
    expect(isStale(pr('x', 1, { committedAt: null }), NOW)).toBe(false);
    expect(matches(rows[3]!, 'stale', NOW)).toBe(true);
  });

  it('keeps stacks together in stack order and the rest in list order', () => {
    expect(groupsOf(snap, 'all', NOW)).toEqual([
      { key: 'a', ids: ['a', 'b'], stacked: true },
      { key: 'c', ids: ['c'], stacked: false },
      { key: 'd', ids: ['d'], stacked: false },
    ]);
    expect(groupsOf(snap, 'draft', NOW).map((g) => g.key)).toEqual(['d']);
    expect(groupsOf(snap, 'blocked', NOW)).toEqual([{ key: 'a', ids: ['b'], stacked: true }]);
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
