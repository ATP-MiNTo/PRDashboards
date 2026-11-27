import { describe, expect, it } from 'vitest';
import type { SnapshotPr } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { searchTerms } from './search';
import type { Group } from './snapshot';
import {
  applyHidden,
  isHideMode,
  isPageSize,
  listStats,
  type ListView,
  pageItems,
  pageRange,
  paginate,
  pruneHidden,
  shownIn,
} from './view';

const g = (key: string, n = 1): Group => ({
  key,
  ids: Array.from({ length: n }, (_, i) => `${key}${i}`),
  stacked: n > 1,
});

describe('paginate', () => {
  it('fills pages to the size', () => {
    const groups = Array.from({ length: 25 }, (_, i) => g(`p${i}`));
    const pages = paginate(groups, 10);
    expect(pages.map((p) => p.length)).toEqual([10, 10, 5]);
  });

  it('never splits a stack, so a page can run over', () => {
    const pages = paginate([g('a', 4), g('b', 4), g('c', 5), g('d', 1)], 10);
    expect(pages.map((p) => p.map((x) => x.key))).toEqual([['a', 'b', 'c'], ['d']]);
  });

  it('changes with the page size', () => {
    const groups = Array.from({ length: 21 }, (_, i) => g(`p${i}`));
    expect(paginate(groups, 15)).toHaveLength(2);
    expect(paginate(groups, 20)).toHaveLength(2);
    expect(paginate(groups, 10)).toHaveLength(3);
  });

  it('gives no pages for nothing', () => {
    expect(paginate([], 10)).toEqual([]);
  });
});

describe('pageRange', () => {
  const pages = paginate([g('a', 4), g('b', 4), g('c', 5), g('d', 1)], 10);

  it('counts pull requests, not groups', () => {
    expect(pageRange(pages, 0)).toEqual({ from: 1, to: 13, total: 14 });
    expect(pageRange(pages, 1)).toEqual({ from: 14, to: 14, total: 14 });
  });

  it('is empty without pages', () => {
    expect(pageRange([], 0)).toEqual({ from: 0, to: 0, total: 0 });
  });
});

describe('pageItems', () => {
  it.each([
    [0, 1, [0]],
    [0, 3, [0, 1, 2]],
    [0, 10, [0, 1, null, 9]],
    [4, 10, [0, null, 3, 4, 5, null, 9]],
    [2, 10, [0, 1, 2, 3, null, 9]],
    [9, 10, [0, null, 8, 9]],
  ])('page %i of %i', (current, count, expected) => {
    expect(pageItems(current, count)).toEqual(expected);
  });
});

describe('hidden', () => {
  const groups = [g('a'), g('s', 3), g('b')];
  const hidden = new Set(['a0', 's1', 'b0']);

  it('visible mode drops hidden ids and empty groups', () => {
    const out = applyHidden(groups, hidden, 'visible');
    expect(out.map((x) => x.ids)).toEqual([['s0', 's2']]);
  });

  it('hidden mode keeps only hidden ids', () => {
    const out = applyHidden(groups, hidden, 'hidden');
    expect(out.map((x) => x.ids)).toEqual([['a0'], ['s1'], ['b0']]);
  });

  it('all mode keeps everything', () => {
    expect(applyHidden(groups, hidden, 'all')).toBe(groups);
  });

  it('says which rows a mode shows', () => {
    expect(shownIn('visible', false)).toBe(true);
    expect(shownIn('visible', true)).toBe(false);
    expect(shownIn('hidden', true)).toBe(true);
    expect(shownIn('hidden', false)).toBe(false);
    expect(shownIn('all', true)).toBe(true);
  });

  it('prunes ids that left the snapshot', () => {
    expect([...pruneHidden(new Set(['a', 'gone']), ['a', 'b'])]).toEqual(['a']);
  });

  it('validates stored values', () => {
    expect(isHideMode('all')).toBe(true);
    expect(isHideMode('nope')).toBe(false);
    expect(isPageSize(15)).toBe(true);
    expect(isPageSize(12)).toBe(false);
    expect(isPageSize('10')).toBe(false);
  });
});

describe('listStats', () => {
  const stackOf = (id: string, position: number): Partial<SnapshotPr> => ({
    stack: {
      id: 'S',
      position,
      size: 2,
      parentId: position === 1 ? null : 'a',
      parentNumber: position === 1 ? null : 1,
    },
  });
  const s = snapshot([
    pr('a', 1, { roles: ['assigned'], ...stackOf('a', 1) }),
    pr('b', 2, {
      roles: ['assigned'],
      ...stackOf('b', 2),
      ci: { state: 'running', total: 0, checks: [] },
    }),
    pr('c', 3, { roles: ['authored'], isDraft: true }),
    pr('d', 4, { roles: ['authored'], ci: { state: 'running', total: 0, checks: [] } }),
  ]);
  const view = (over: Partial<ListView> = {}): ListView => ({
    filter: 'all',
    show: 'everything',
    terms: [],
    hidden: new Set(),
    mode: 'visible',
    ...over,
  });

  it('counts what the list holds: showing, ready, running and stacks', () => {
    expect(listStats(s, view())).toEqual({ showing: 4, ready: 1, running: 2, stacks: 1 });
  });

  it('follows the Show state', () => {
    expect(listStats(s, view({ show: 'authored' }))).toEqual({
      showing: 2,
      ready: 0,
      running: 1,
      stacks: 0,
    });
  });

  it('follows the search', () => {
    expect(listStats(s, view({ terms: searchTerms('change 3') })).showing).toBe(1);
  });

  it('leaves out the hidden pull requests, and keeps only them in the Hidden mode', () => {
    expect(listStats(s, view({ hidden: new Set(['d']) })).showing).toBe(3);
    expect(listStats(s, view({ hidden: new Set(['d']), mode: 'all' })).showing).toBe(4);
    expect(listStats(s, view({ hidden: new Set(['d']), mode: 'hidden' })).showing).toBe(1);
  });

  it('keeps the other tiles whole when a tile filters the list, and narrows Showing', () => {
    const f = listStats(s, view({ filter: 'running' }));
    expect(f.showing).toBe(2);
    expect(f.ready).toBe(1);
    expect(listStats(s, view({ filter: 'stacks' })).showing).toBe(2);
    expect(listStats(s, view({ filter: 'ready' })).showing).toBe(1);
  });
});
