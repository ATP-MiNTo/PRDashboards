import { describe, expect, it } from 'vitest';
import { pr } from '@/test-fixtures';
import type { SnapshotPr } from '@/api/client';
import { groupsOf, type Group } from './snapshot';
import { snapshot } from '@/test-fixtures';
import { effectiveDir, naturalDir, sortGroups, type SortKey } from './sort';

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
  const groups = groupsOf(snapshot(prs), 'all', Date.now());
  const map = new Map(prs.map((p) => [p.id, p]));
  return sortGroups(groups, map, key, reversed).map((g) => g.ids);
}

const prs = () => [
  single('a', 5, { title: 'banana', committedAt: '2026-11-20T10:00:00Z' }),
  single('b', 2, {
    title: 'Apple',
    committedAt: '2026-11-24T10:00:00Z',
    repo: { ...pr('z', 1).repo, nameWithOwner: 'acme/zeta' },
  }),
  single('c', 9, { title: 'cherry', committedAt: null, isDraft: true }),
  single('d', 7, {
    title: 'date',
    committedAt: '2026-11-22T10:00:00Z',
    ci: { state: 'failing', total: 0, checks: [] },
  }),
];

describe('sortGroups', () => {
  it.each<[SortKey, string[]]>([
    ['updated', ['b', 'd', 'a', 'c']],
    ['title', ['b', 'a', 'c', 'd']],
    ['repo', ['a', 'd', 'c', 'b']],
    ['number', ['b', 'a', 'd', 'c']],
    ['ready', ['a', 'b', 'd', 'c']],
  ])('sorts by %s in its natural order', (key, order) => {
    expect(run(prs(), key).map((g) => g[0])).toEqual(order);
  });

  it.each<[SortKey, string[]]>([
    ['updated', ['c', 'a', 'd', 'b']],
    ['title', ['d', 'c', 'a', 'b']],
    ['number', ['c', 'd', 'a', 'b']],
    ['ready', ['c', 'd', 'a', 'b']],
  ])('reverses %s', (key, order) => {
    expect(run(prs(), key, true).map((g) => g[0])).toEqual(order);
  });

  it('keeps equal keys in the incoming order', () => {
    const same = [single('a', 1), single('b', 1), single('c', 1)];
    expect(run(same, 'number').flat()).toEqual(['a', 'b', 'c']);
    expect(run(same, 'number', true).flat()).toEqual(['a', 'b', 'c']);
  });

  it('keeps a stack together, in stack order, ranked by its best member', () => {
    const rows = [
      single('x', 3, { committedAt: '2026-11-21T10:00:00Z' }),
      stack('s1', 1, 2, { number: 10, committedAt: '2026-11-10T10:00:00Z' }),
      stack('s2', 2, 2, { number: 11, committedAt: '2026-11-23T10:00:00Z' }),
      single('y', 4, { committedAt: '2026-11-22T10:00:00Z' }),
    ];
    expect(run(rows, 'updated')).toEqual([['s1', 's2'], ['y'], ['x']]);
    // Reversed: the stack's best member is now its oldest one.
    expect(run(rows, 'updated', true)).toEqual([['s1', 's2'], ['x'], ['y']]);
  });

  it('ranks a stack by its first member under the number sort', () => {
    const rows = [
      single('x', 5),
      stack('s1', 1, 2, { number: 10 }),
      stack('s2', 2, 2, { number: 2 }),
    ];
    expect(run(rows, 'number')).toEqual([['s1', 's2'], ['x']]);
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

describe('direction', () => {
  it('starts newest and best first, text ascending', () => {
    expect(naturalDir('updated')).toBe('desc');
    expect(naturalDir('ready')).toBe('desc');
    expect(naturalDir('title')).toBe('asc');
  });

  it('flips when reversed', () => {
    expect(effectiveDir('updated', true)).toBe('asc');
    expect(effectiveDir('title', true)).toBe('desc');
    expect(effectiveDir('title', false)).toBe('asc');
  });
});
