import { describe, expect, it } from 'vitest';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { newRule } from './rules';
import {
  MODE_CHOICES,
  modeChoices,
  modeLabel,
  MULTI_PR_CHOICES,
  MULTI_PR_HINT,
  preferList,
  projectKey,
  projectsOf,
  ruleName,
  ruleProblem,
  statusesOf,
  statusKey,
  TRIGGERS,
  triggerLabel,
  withCurrent,
} from './rules';

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

describe('triggers', () => {
  it('lists every trigger of the API once, the two approval triggers with their words', () => {
    expect(TRIGGERS.map((t) => t.value).sort()).toEqual(
      [
        'changes_requested',
        'ci_failing',
        'ci_passing',
        'commit_stale',
        'merged',
        'no_review_yet',
        'ready_for_review',
        'review_approved',
        'review_requested',
        'review_submitted',
        'waiting_approvals',
      ].sort(),
    );
    expect(triggerLabel('no_review_yet')).toBe('No review yet');
    expect(triggerLabel('waiting_approvals')).toBe(
      'The pull request is waiting for more approvals',
    );
  });

  it('names an unnamed rule after its trigger and target', () => {
    expect(ruleName({ ...newRule(), trigger: 'no_review_yet', target: 'In Review' })).toBe(
      'No review yet to In Review',
    );
  });

  it('needs no hours for the new triggers, only a target', () => {
    for (const trigger of ['no_review_yet', 'waiting_approvals'] as const) {
      expect(ruleProblem({ ...newRule(), trigger, target: 'In Review' })).toBeNull();
      expect(ruleProblem({ ...newRule(), trigger, target: '' })).toBe(
        'Choose the status to move tickets to.',
      );
    }
  });
});

describe('the mode of shared tickets', () => {
  it('words the two modes of the API for the "Tickets shared by several PRs" select', () => {
    expect(MODE_CHOICES).toEqual([
      { value: 'all', label: 'When the last PR meets it' },
      { value: 'any', label: 'When the first PR meets it' },
    ]);
    expect(MULTI_PR_HINT).toBe(
      'When a ticket is linked to several PRs. Default: follows "Multi-PR tickets move when" in the Repositories setting of the sidebar. All: the ticket moves only when every linked PR meets the rule. Any: the first PR that meets it moves the ticket.',
    );
  });

  it('words the global setting of the sidebar', () => {
    expect(MULTI_PR_CHOICES).toEqual([
      { value: 'all', label: 'All PRs are merged' },
      { value: 'any', label: 'First PR is merged' },
    ]);
  });

  it('offers the default first, worded with what the setting is now', () => {
    expect(modeChoices('all')).toEqual([
      { value: 'default', label: 'Default (when the last PR meets it)' },
      { value: 'all', label: 'When the last PR meets it' },
      { value: 'any', label: 'When the first PR meets it' },
    ]);
    expect(modeChoices('any')[0]).toEqual({
      value: 'default',
      label: 'Default (when the first PR meets it)',
    });
  });

  it('starts a new rule in the mode that follows the setting', () => {
    expect(newRule().mode).toBe('default');
    expect(modeLabel('default')).toBe('Default');
    expect(modeLabel('all')).toBe('All');
    expect(modeLabel('any')).toBe('Any');
  });
});
