import { describe, expect, it } from 'vitest';
import type { SnapshotPr } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { groupsOf } from './snapshot';
import { inShow, isShowMode, rolesOf, roleKind, showCounts, type Role } from './show';

const withRoles = (id: string, roles: Role[]) => pr(id, 1, { roles });

describe('rolesOf', () => {
  it('returns the roles of the pull request', () => {
    expect(rolesOf(withRoles('a', ['authored']))).toEqual(['authored']);
  });

  it.each([
    ['empty', []],
    ['missing', undefined],
  ])('counts a pull request with %s roles for both', (_name, roles) => {
    const old = { ...pr('a', 1), roles } as unknown as SnapshotPr;
    expect(rolesOf(old)).toEqual(['assigned', 'authored']);
  });
});

describe('roleKind', () => {
  it.each([
    [['assigned'], 'assigned'],
    [['authored'], 'authored'],
    [['assigned', 'authored'], 'both'],
    [['authored', 'assigned'], 'both'],
  ] as [Role[], string][])('%j is %s', (roles, kind) => {
    expect(roleKind(withRoles('a', roles))).toBe(kind);
  });
});

describe('inShow', () => {
  const assigned = withRoles('a', ['assigned']);
  const authored = withRoles('o', ['authored']);
  const both = withRoles('b', ['assigned', 'authored']);

  it.each([
    ['everything', assigned, true],
    ['everything', authored, true],
    ['everything', both, true],
    ['assigned', assigned, true],
    ['assigned', authored, false],
    ['assigned', both, true],
    ['authored', assigned, false],
    ['authored', authored, true],
    ['authored', both, true],
  ] as const)('%s with %j', (mode, row, shown) => {
    expect(inShow(row, mode)).toBe(shown);
  });
});

describe('showCounts', () => {
  it('counts every mode across the whole snapshot', () => {
    const s = snapshot([
      withRoles('a', ['assigned']),
      withRoles('b', ['assigned']),
      withRoles('o', ['authored']),
      withRoles('x', ['assigned', 'authored']),
    ]);
    expect(showCounts(s)).toEqual({ everything: 4, assigned: 3, authored: 2 });
  });

  it('is zero for an empty snapshot', () => {
    expect(showCounts(snapshot([]))).toEqual({ everything: 0, assigned: 0, authored: 0 });
  });
});

describe('isShowMode', () => {
  it('accepts the three modes only', () => {
    expect(['everything', 'assigned', 'authored'].every(isShowMode)).toBe(true);
    expect(isShowMode('both')).toBe(false);
    expect(isShowMode(null)).toBe(false);
  });
});

describe('groupsOf with a Show mode', () => {
  const s = snapshot([
    withRoles('a', ['assigned']),
    withRoles('o', ['authored']),
    withRoles('b', ['assigned', 'authored']),
  ]);
  const ids = (mode: Parameters<typeof groupsOf>[2]) =>
    groupsOf(s, 'all', mode).flatMap((g) => g.ids);

  it('keeps the pull requests of the mode, in snapshot order', () => {
    expect(ids('everything')).toEqual(['a', 'o', 'b']);
    expect(ids('assigned')).toEqual(['a', 'b']);
    expect(ids('authored')).toEqual(['o', 'b']);
  });

  it('defaults to everything', () => {
    expect(groupsOf(s, 'all').flatMap((g) => g.ids)).toEqual(['a', 'o', 'b']);
  });

  it('keeps only the members of a stack that the mode lists', () => {
    const stack = (n: number, roles: Role[]) =>
      pr(`s${n}`, n, {
        roles,
        stack: { id: 's1', position: n, size: 2, parentId: null, parentNumber: null },
      });
    const g = groupsOf(
      snapshot([stack(1, ['assigned']), stack(2, ['authored'])]),
      'all',
      'authored',
    );
    expect(g).toEqual([{ key: 's1', ids: ['s2'], stacked: true }]);
  });
});
