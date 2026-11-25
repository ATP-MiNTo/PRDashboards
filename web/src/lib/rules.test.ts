import { describe, expect, it } from 'vitest';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { preferList, projectsOf, statusesOf, withCurrent } from './rules';

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

  it('prefers the Jira list and falls back when it is empty or missing', () => {
    expect(preferList(['X'], ['Y'])).toEqual(['X']);
    expect(preferList([], ['Y'])).toEqual(['Y']);
    expect(preferList(undefined, ['Y'])).toEqual(['Y']);
  });
});
