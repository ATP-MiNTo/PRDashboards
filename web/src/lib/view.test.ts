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
  locate,
  type ListView,
  pageItems,
  pageRange,
  pinLaunched,
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
    off: new Set(),
    ...over,
  });

  it('counts what the list holds: showing, ready, running and stacks', () => {
    expect(listStats(s, view())).toEqual({ showing: 4, ready: 1, running: 2, stacks: 1 });
  });

  it('leaves out the repositories that are switched off', () => {
    const other = (id: string, n: number) =>
      pr(id, n, {
        repo: {
          id: 'R2',
          nameWithOwner: 'acme/api',
          isNew: false,
          squashAllowed: true,
          mergeAllowed: true,
          rebaseAllowed: true,
        },
      });
    const two = snapshot([pr('a', 1), other('b', 2), other('c', 3)]);
    expect(listStats(two, view({ off: new Set(['R2']) })).showing).toBe(1);
    expect(listStats(two, view({ off: new Set(['R1']) })).showing).toBe(2);
    expect(listStats(two, view({ off: new Set(['R1', 'R2']) })).showing).toBe(0);
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

describe('locate and pinLaunched', () => {
  const groups = [g('s', 3), g('x', 1), g('y', 1)];

  it('finds where a card is', () => {
    expect(locate(groups, 's1')).toEqual({ groupKey: 's', stacked: true, groupIndex: 0, index: 1 });
    expect(locate(groups, 'y0')).toEqual({
      groupKey: 'y',
      stacked: false,
      groupIndex: 2,
      index: 0,
    });
    expect(locate(groups, 'nope')).toBeNull();
  });

  it('leaves the groups alone while the snapshot still has every launched card', () => {
    const l = { id: 'x0', ...locate(groups, 'x0')! };
    expect(pinLaunched(groups, [l], new Set(['s0', 's1', 's2', 'x0', 'y0']))).toBe(groups);
  });

  it('puts a dropped single card back where it was', () => {
    const l = { id: 'x0', ...locate(groups, 'x0')! };
    const now = [g('s', 3), g('y', 1)];
    const out = pinLaunched(now, [l], new Set(['s0', 's1', 's2', 'y0']));
    expect(out.map((x) => x.key)).toEqual(['s', 'x0', 'y']);
    expect(now).toHaveLength(2);
  });

  it('puts a dropped card of a stack back into that stack', () => {
    const l = { id: 's1', ...locate(groups, 's1')! };
    const now = [{ key: 's', ids: ['s0', 's2'], stacked: true }, g('x', 1)];
    const out = pinLaunched(now, [l], new Set(['s0', 's2', 'x0']));
    expect(out[0]?.ids).toEqual(['s0', 's1', 's2']);
    expect(now[0]?.ids).toEqual(['s0', 's2']);
  });

  it('puts a card of a stack that is gone back alone', () => {
    const l = { id: 's0', ...locate(groups, 's0')! };
    const out = pinLaunched([g('x', 1)], [l], new Set(['x0']));
    expect(out.map((x) => x.ids)).toEqual([['s0'], ['x0']]);
    expect(out[0]?.stacked).toBe(false);
  });
});
