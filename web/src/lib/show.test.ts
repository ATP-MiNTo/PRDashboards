import { describe, expect, it } from 'vitest';
import type { SnapshotPr } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { groupsOf } from './snapshot';
import {
  inShow,
  isShowMode,
  parseShowMode,
  rolesOf,
  roleKind,
  showCounts,
  type Role,
} from './show';

const withRoles = (id: string, roles: Role[]) => pr(id, 1, { roles });

describe('rolesOf', () => {
  it('returns the roles of the pull request', () => {
    expect(rolesOf(withRoles('a', ['authored']))).toEqual(['authored']);
  });

  it.each([
    ['empty', []],
    ['missing', undefined],
  ])('counts a pull request with %s roles as assigned and authored', (_name, roles) => {
    const old = { ...pr('a', 1), roles } as unknown as SnapshotPr;
    expect(rolesOf(old)).toEqual(['assigned', 'authored']);
  });
});

describe('roleKind', () => {
  it.each([
    [['assigned'], 'assigned'],
    [['authored'], 'assigned'],
    [['assigned', 'authored'], 'assigned'],
    [['reviewer'], 'reviewer'],
    [['assigned', 'reviewer'], 'both'],
    [['reviewer', 'authored'], 'both'],
    [['assigned', 'authored', 'reviewer'], 'both'],
  ] as [Role[], string][])('%j is %s', (roles, kind) => {
    expect(roleKind(withRoles('a', roles))).toBe(kind);
  });
});

describe('inShow', () => {
  const assigned = withRoles('a', ['assigned']);
  const authored = withRoles('o', ['authored']);
  const reviewer = withRoles('r', ['reviewer']);
  const mixed = withRoles('m', ['authored', 'reviewer']);

  it.each([
    ['all', assigned, true],
    ['all', authored, true],
    ['all', reviewer, true],
    ['mine', assigned, true],
    ['mine', authored, true],
    ['mine', reviewer, false],
    ['mine', mixed, true],
    ['review', assigned, false],
    ['review', authored, false],
    ['review', reviewer, true],
    ['review', mixed, true],
  ] as const)('%s with %j', (mode, row, shown) => {
    expect(inShow(row, mode)).toBe(shown);
  });

  it('lists a pull request without roles under All and Mine, never under To review', () => {
    const old = { ...pr('a', 1), roles: undefined } as unknown as SnapshotPr;
    expect([inShow(old, 'all'), inShow(old, 'mine'), inShow(old, 'review')]).toEqual([
      true,
      true,
      false,
    ]);
  });
});

describe('showCounts', () => {
  it('counts every mode across the whole snapshot', () => {
    const s = snapshot([
      withRoles('a', ['assigned']),
      withRoles('b', ['assigned']),
      withRoles('o', ['authored']),
      withRoles('x', ['assigned', 'authored']),
      withRoles('r', ['reviewer']),
      withRoles('y', ['authored', 'reviewer']),
    ]);
    expect(showCounts(s)).toEqual({ all: 6, mine: 5, review: 2 });
  });

  it('leaves out the repositories that are switched off', () => {
    const away = withRoles('r', ['reviewer']);
    away.repo = { ...away.repo, id: 'R2' };
    const s = snapshot([withRoles('a', ['assigned']), away]);
    expect(showCounts(s, new Set(['R2']))).toEqual({ all: 1, mine: 1, review: 0 });
    expect(showCounts(s, new Set(['R1', 'R2']))).toEqual({ all: 0, mine: 0, review: 0 });
  });

  it('is zero for an empty snapshot', () => {
    expect(showCounts(snapshot([]))).toEqual({ all: 0, mine: 0, review: 0 });
  });
});

describe('isShowMode', () => {
  it('accepts the three modes only', () => {
    expect(['all', 'mine', 'review'].every(isShowMode)).toBe(true);
    expect(isShowMode('both')).toBe(false);
    expect(isShowMode('everything')).toBe(false);
    expect(isShowMode(null)).toBe(false);
  });
});

describe('parseShowMode', () => {
  it.each([
    ['all', 'all'],
    ['mine', 'mine'],
    ['review', 'review'],
    ['everything', 'all'],
    ['assigned', 'mine'],
    ['authored', 'mine'],
    ['nope', 'all'],
    ['', 'all'],
    [null, 'all'],
    [undefined, 'all'],
  ])('%j is %s', (stored, mode) => {
    expect(parseShowMode(stored)).toBe(mode);
  });
});

describe('groupsOf with a Show mode', () => {
  const s = snapshot([
    withRoles('a', ['assigned']),
    withRoles('o', ['authored']),
    withRoles('b', ['assigned', 'authored']),
    withRoles('r', ['reviewer']),
    withRoles('m', ['authored', 'reviewer']),
  ]);
  const ids = (mode: Parameters<typeof groupsOf>[2]) =>
    groupsOf(s, 'all', mode).flatMap((g) => g.ids);

  it('keeps the pull requests of the mode, in snapshot order', () => {
    expect(ids('all')).toEqual(['a', 'o', 'b', 'r', 'm']);
    expect(ids('mine')).toEqual(['a', 'o', 'b', 'm']);
    expect(ids('review')).toEqual(['r', 'm']);
  });

  it('defaults to all', () => {
    expect(groupsOf(s, 'all').flatMap((g) => g.ids)).toEqual(['a', 'o', 'b', 'r', 'm']);
  });

  it('keeps only the members of a stack that the mode lists', () => {
    const stack = (n: number, roles: Role[]) =>
      pr(`s${n}`, n, {
        roles,
        stack: { id: 's1', position: n, size: 2, parentId: null, parentNumber: null },
      });
    const g = groupsOf(snapshot([stack(1, ['assigned']), stack(2, ['reviewer'])]), 'all', 'review');
    expect(g).toEqual([{ key: 's1', ids: ['s2'], stacked: true }]);
  });
});
