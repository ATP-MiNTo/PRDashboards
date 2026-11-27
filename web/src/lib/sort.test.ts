import { describe, expect, it } from 'vitest';
import { pr } from '@/test-fixtures';
import type { SnapshotPr } from '@/api/client';
import { groupsOf, type Group } from './snapshot';
import { snapshot } from '@/test-fixtures';
import { sizeOf, sortGroups, type SortKey } from './sort';

const stack = (id: string, position: number, size: number, over: Partial<SnapshotPr> = {}) =>
  pr(id, over.number ?? 0, {
    ...over,
    stack: {
      id: 'S',
      position,
      size,
      parentId: position === 1 ? null : 'x',
      parentNumber: position === 1 ? null : 1,
    },
  });

const single = (id: string, number: number, over: Partial<SnapshotPr> = {}) => pr(id, number, over);

function run(prs: SnapshotPr[], key: SortKey, reversed = false): string[][] {
  const groups = groupsOf(snapshot(prs), 'all');
  const map = new Map(prs.map((p) => [p.id, p]));
  return sortGroups(groups, map, key, reversed).map((g) => g.ids);
}

const prs = () => [
  single('a', 5, {
    title: 'banana',
    updatedAt: '2026-11-20T10:00:00Z',
    additions: 10,
    deletions: 5,
  }),
  single('b', 2, {
    title: 'Apple',
    updatedAt: '2026-11-24T10:00:00Z',
    additions: 100,
    deletions: 50,
    repo: { ...pr('z', 1).repo, nameWithOwner: 'acme/zeta' },
  }),
  single('c', 9, {
    title: 'cherry',
    updatedAt: null,
    committedAt: null,
    additions: null,
    deletions: null,
    isDraft: true,
  }),
  single('d', 7, {
    title: 'date',
    updatedAt: '2026-11-22T10:00:00Z',
    additions: 20,
    deletions: 0,
    ci: { state: 'failing', total: 0, checks: [] },
  }),
];

describe('sortGroups', () => {
  it.each<[SortKey, string[]]>([
    ['updated', ['b', 'd', 'a', 'c']],
    ['title', ['b', 'a', 'c', 'd']],
    ['repo', ['a', 'd', 'c', 'b']],
    ['size', ['b', 'd', 'a', 'c']],
    ['ready', ['a', 'b', 'd', 'c']],
  ])('sorts by %s in its natural order', (key, order) => {
    expect(run(prs(), key).map((g) => g[0])).toEqual(order);
  });

  it.each<[SortKey, string[]]>([
    ['updated', ['c', 'a', 'd', 'b']],
    ['title', ['d', 'c', 'a', 'b']],
    ['size', ['c', 'a', 'd', 'b']],
    ['ready', ['c', 'd', 'a', 'b']],
  ])('reverses %s', (key, order) => {
    expect(run(prs(), key, true).map((g) => g[0])).toEqual(order);
  });

  it('keeps equal keys in the incoming order', () => {
    const same = [single('a', 1), single('b', 1), single('c', 1)];
    expect(run(same, 'size').flat()).toEqual(['a', 'b', 'c']);
    expect(run(same, 'size', true).flat()).toEqual(['a', 'b', 'c']);
  });

  it('keeps a stack together, in stack order, ranked by its best member', () => {
    const rows = [
      single('x', 3, { updatedAt: '2026-11-21T10:00:00Z' }),
      stack('s1', 1, 2, { number: 10, updatedAt: '2026-11-10T10:00:00Z' }),
      stack('s2', 2, 2, { number: 11, updatedAt: '2026-11-23T10:00:00Z' }),
      single('y', 4, { updatedAt: '2026-11-22T10:00:00Z' }),
    ];
    expect(run(rows, 'updated')).toEqual([['s1', 's2'], ['y'], ['x']]);
    // Reversed: the stack's best member is now its oldest one.
    expect(run(rows, 'updated', true)).toEqual([['s1', 's2'], ['x'], ['y']]);
  });

  it('ranks a stack by its biggest member under the size sort', () => {
    const rows = [
      single('x', 5, { additions: 50, deletions: 0 }),
      stack('s1', 1, 2, { number: 10, additions: 1, deletions: 0 }),
      stack('s2', 2, 2, { number: 2, additions: 100, deletions: 0 }),
    ];
    expect(run(rows, 'size')).toEqual([['s1', 's2'], ['x']]);
  });

  it('falls back to the last commit when the update time is missing', () => {
    const rows = [
      single('old', 1, { updatedAt: null, committedAt: '2026-11-01T10:00:00Z' }),
      single('new', 2, { updatedAt: null, committedAt: '2026-11-20T10:00:00Z' }),
      single('mid', 3, { updatedAt: '2026-11-10T10:00:00Z' }),
    ];
    expect(run(rows, 'updated').flat()).toEqual(['new', 'mid', 'old']);
  });

  it('ignores ids that are not in the map', () => {
    const groups: Group[] = [
      { key: 'gone', ids: ['gone'], stacked: false },
      { key: 'a', ids: ['a'], stacked: false },
    ];
    const out = sortGroups(groups, new Map([['a', single('a', 1)]]), 'title', false);
    expect(out.map((g) => g.key)).toEqual(['gone', 'a']);
  });
});

describe('sizeOf', () => {
  it('adds the lines, and counts 0 for a snapshot that does not say', () => {
    expect(sizeOf({ additions: 12, deletions: 3 })).toBe(15);
    expect(sizeOf({ additions: null, deletions: null })).toBe(0);
    expect(sizeOf({ additions: 7, deletions: null })).toBe(7);
  });
});
