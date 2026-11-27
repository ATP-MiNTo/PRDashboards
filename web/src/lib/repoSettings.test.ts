import { describe, expect, it } from 'vitest';
import type { RepoSettings } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import {
  allowedOf,
  branchesOf,
  deleteDefaultOf,
  deleteLabel,
  DEFAULT_APPROVALS,
  draftChanged,
  draftOf,
  draftValid,
  isCustom,
  methodChoices,
  methodLabel,
  MIN_APPROVALS,
  SEED_RULES,
  stepApprovals,
  updateOf,
  validPattern,
  withMethod,
} from './repoSettings';

const repo = (over: Partial<RepoSettings> = {}): RepoSettings => ({
  id: 'R1',
  nameWithOwner: 'acme/web',
  isNew: false,
  openPrs: 1,
  defaultMethod: 'squash',
  inheritMethod: true,
  deleteBranch: 'default',
  approvalsRequired: 0,
  branchRules: [...SEED_RULES],
  ...over,
});

describe('validPattern', () => {
  it('takes letters, digits and . _ / * - up to 200 characters', () => {
    for (const ok of ['main', 'release/*', 'a.b_c-9', '*']) expect(validPattern(ok)).toBe(true);
    for (const bad of ['', 'a b', 'a(b)', 'x'.repeat(201), 'é'])
      expect(validPattern(bad)).toBe(false);
  });
});

describe('the draft of a repository', () => {
  it('reads "use default" from a repository that follows the default and keeps its own method', () => {
    const d = draftOf(repo({ defaultMethod: 'rebase', inheritMethod: true }));
    expect(d.method).toBe('default');
    expect(d.ownMethod).toBe('rebase');
    expect(draftOf(repo({ inheritMethod: false })).method).toBe('squash');
  });

  it('saves the method it has, or the kept one with the follow flag', () => {
    const own = updateOf({ ...draftOf(repo()), method: 'merge' });
    expect(own).toMatchObject({ defaultMethod: 'merge', inheritMethod: false, clearNew: true });
    const follow = updateOf(draftOf(repo({ defaultMethod: 'rebase' })));
    expect(follow).toMatchObject({ defaultMethod: 'rebase', inheritMethod: true });
    expect(follow).toMatchObject({ deleteBranch: 'default', approvalsRequired: 0 });
    expect(follow.branchRules).toEqual(SEED_RULES);
  });

  it('is changed by any of its choices, and by an own method only when it is in use', () => {
    const r = repo();
    const d = draftOf(r);
    expect(draftChanged(d, r)).toBe(false);
    expect(draftChanged({ ...d, deleteBranch: 'no' }, r)).toBe(true);
    expect(draftChanged({ ...d, approvals: 2 }, r)).toBe(true);
    expect(draftChanged({ ...d, method: 'merge' }, r)).toBe(true);
    expect(draftChanged({ ...d, rules: [] }, r)).toBe(true);
    // The kept method does not matter while the repository follows the default.
    expect(draftChanged({ ...d, ownMethod: 'rebase' }, r)).toBe(false);
    const own = repo({ inheritMethod: false });
    expect(draftChanged({ ...draftOf(own), ownMethod: 'rebase' }, own)).toBe(false);
    expect(draftChanged({ ...draftOf(own), method: 'rebase' }, own)).toBe(true);
  });

  it('is valid with good patterns and at most 50 rules', () => {
    const d = draftOf(repo());
    expect(draftValid(d)).toBe(true);
    expect(draftValid({ ...d, rules: [{ pattern: '', method: 'merge' }] })).toBe(false);
    const many = Array.from({ length: 51 }, () => ({ pattern: 'a', method: 'merge' as const }));
    expect(draftValid({ ...d, rules: many })).toBe(false);
    expect(draftValid({ ...d, rules: many.slice(1) })).toBe(true);
  });
});

describe('the rolls', () => {
  it('says the default and what it is now', () => {
    expect(deleteLabel('default', true)).toBe('Use default (Yes)');
    expect(deleteLabel('default', false)).toBe('Use default (No)');
    expect(deleteLabel('default', undefined)).toBe('Use default');
    expect(deleteLabel('yes', false)).toBe('Yes');
    expect(deleteLabel('no', true)).toBe('No');
    expect(methodLabel('default', 'squash')).toBe('Use default (Squash)');
    expect(methodLabel('default', undefined)).toBe('Use default');
    expect(methodLabel('merge', 'squash')).toBe('Merge commit');
  });

  it('turns through the default and the methods the repository allows', () => {
    const all = { squash: true, merge: true, rebase: true };
    expect(methodChoices(all, 'default')).toEqual(['default', 'squash', 'merge', 'rebase']);
    const noSquash = { squash: false, merge: true, rebase: true };
    expect(methodChoices(noSquash, 'default')).toEqual(['default', 'merge', 'rebase']);
    // A method that is no longer allowed stays while it is the choice.
    expect(methodChoices(noSquash, 'squash')).toEqual(['default', 'squash', 'merge', 'rebase']);
  });

  it('reads what a repository allows from its pull requests, and allows all when it has none', () => {
    const locked = pr('a', 1);
    locked.repo = { ...locked.repo, squashAllowed: false, rebaseAllowed: false };
    const s = snapshot([locked]);
    expect(allowedOf(s, 'R1')).toEqual({ squash: false, merge: true, rebase: false });
    expect(allowedOf(s, 'R9')).toEqual({ squash: true, merge: true, rebase: true });
  });
});

describe('stepApprovals', () => {
  it('steps by one and stays between 0 and 10', () => {
    expect(MIN_APPROVALS).toBe(0);
    expect(DEFAULT_APPROVALS).toBe(0);
    expect(stepApprovals(1, 1)).toBe(2);
    expect(stepApprovals(5, -1)).toBe(4);
    expect(stepApprovals(1, -1)).toBe(0);
    expect(stepApprovals(0, -1)).toBe(0);
    expect(stepApprovals(0, 1)).toBe(1);
    expect(stepApprovals(10, 1)).toBe(10);
  });
});

describe('the two states of a roll', () => {
  it('stands for the default delete-branch choice with a value, No while it is unknown', () => {
    expect(deleteDefaultOf(true)).toBe('yes');
    expect(deleteDefaultOf(false)).toBe('no');
    expect(deleteDefaultOf(undefined)).toBe('no');
  });

  it('keeps the method that was chosen while the default is on, and takes it back', () => {
    const d = draftOf(repo({ inheritMethod: false, defaultMethod: 'squash' }));
    const own = withMethod(d, 'rebase');
    expect(own).toMatchObject({ method: 'rebase', ownMethod: 'rebase' });
    const dflt = withMethod(own, 'default');
    expect(dflt).toMatchObject({ method: 'default', ownMethod: 'rebase' });
    expect(updateOf(dflt)).toMatchObject({ defaultMethod: 'rebase', inheritMethod: true });
    // Already on the default: nothing changes.
    expect(withMethod(dflt, 'default')).toBe(dflt);
    expect(withMethod(dflt, 'merge')).toMatchObject({ method: 'merge', ownMethod: 'merge' });
  });
});

describe('isCustom', () => {
  it('is false for a repository that has only the defaults', () => {
    expect(isCustom(repo())).toBe(false);
  });

  it('does not depend on the order of the keys of a rule, as the server writes them', () => {
    const fromServer = JSON.parse(
      '[{"method":"merge","pattern":"main"},{"method":"merge","pattern":"production"}]',
    ) as RepoSettings['branchRules'];
    expect(isCustom(repo({ branchRules: fromServer }))).toBe(false);
    expect(draftChanged(draftOf(repo({ branchRules: fromServer })), repo())).toBe(false);
  });

  it.each([
    ['a delete-branch choice', { deleteBranch: 'no' as const }],
    ['a method of its own', { inheritMethod: false }],
    ['one approval required', { approvalsRequired: 1 }],
    ['more approvals', { approvalsRequired: 2 }],
    ['other branch rules', { branchRules: [{ pattern: 'release/*', method: 'rebase' as const }] }],
    ['no branch rules', { branchRules: [] }],
  ])('is true with %s', (_name, over) => {
    expect(isCustom(repo(over))).toBe(true);
  });
});

describe('branchesOf', () => {
  it('lists the base and head branches of a repository, sorted and once', () => {
    const s = snapshot([
      pr('a', 1, { baseRef: 'main', headRef: 'feat/a' }),
      pr('b', 2, { baseRef: 'main', headRef: 'feat/b' }),
    ]);
    expect(branchesOf(s, 'R1')).toEqual(['feat/a', 'feat/b', 'main']);
    expect(branchesOf(s, 'R2')).toEqual([]);
  });
});
