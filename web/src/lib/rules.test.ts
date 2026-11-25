import { describe, expect, it } from 'vitest';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { preferList, projectKey, projectsOf, statusesOf, statusKey, withCurrent } from './rules';

describe('rule form choices', () => {
  const snap = snapshot([
    pr('a', 1, { tickets: [ticket('OPS-1', { status: 'To Do' }), ticket('INP-1')] }),
    pr('b', 2, { tickets: [ticket('INP-2', { status: 'To Do' })] }),
  ]);

  it('lists the distinct sorted projects of the snapshot', () => {
    expect(projectsOf(snap)).toEqual(['INP', 'OPS']);
    expect(projectsOf(undefined)).toEqual([]);
    expect(statusesOf(snap)).toEqual(['In Review', 'To Do']);
  });

  it('adds the current value only when it is set and missing', () => {
    expect(withCurrent(['A', 'B'], 'C')).toEqual(['A', 'B', 'C']);
    expect(withCurrent(['A', 'B'], 'A')).toEqual(['A', 'B']);
    expect(withCurrent(['A'], '')).toEqual(['A']);
  });

  it('treats near-duplicate statuses as one', () => {
    expect(withCurrent(['Reopen', 'Done'], 'Re-open')).toEqual(['Re-open', 'Done']);
    expect(withCurrent(['Reopened'], 'Re-open')).toEqual(['Reopened', 'Re-open']);
    expect(withCurrent(['---'], '--')).toEqual(['---', '--']);
    const dup = snapshot([
      pr('a', 1, {
        tickets: [ticket('A-1', { status: 'Re-open' }), ticket('A-2', { status: 're open' })],
      }),
      pr('b', 2, {
        tickets: [ticket('A-3', { status: 'REOPEN' }), ticket('A-4', { status: 'Done' })],
      }),
    ]);
    expect(statusesOf(dup)).toEqual(['Done', 'Re-open']);
  });

  it('keeps projects apart unless they differ only in case', () => {
    expect(withCurrent(['AB'], 'A_B', projectKey)).toEqual(['AB', 'A_B']);
    expect(withCurrent(['A_B', 'AB'], 'ab', projectKey)).toEqual(['A_B', 'ab']);
  });

  it('prefers the Jira list and falls back when it is empty or missing', () => {
    expect(preferList(['X'], ['Y'])).toEqual(['X']);
    expect(preferList([], ['Y'])).toEqual(['Y']);
    expect(preferList(undefined, ['Y'])).toEqual(['Y']);
  });
});

describe('statusKey', () => {
  it.each([
    ['Re-open', 'reopen'],
    ['re open', 'reopen'],
    ['REOPEN', 'reopen'],
    ['Reopened', 'reopened'],
    ['  In   Progress! ', 'inprogress'],
    ['Phase 2', 'phase2'],
    ['สถานะ 1', 'สถานะ1'],
    ['---', '---'],
    [' -- ', '--'],
    ['', ''],
  ])('%j gives %j', (name, key) => {
    expect(statusKey(name)).toBe(key);
  });
});
