import { describe, expect, it } from 'vitest';
import type { RulePreview, SyncRule } from '@/api/client';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { newRule, TRIGGERS } from './rules';
import {
  dropLineY,
  dropSlot,
  mergeTable,
  previewKey,
  previewTable,
  reorder,
  RULE_TEMPLATES,
  ruleChanged,
  ruleFromTemplate,
  sameRule,
} from './rulesEditor';

const rule = (id: string, over: Partial<SyncRule> = {}): SyncRule => ({
  ...newRule(),
  id,
  name: id,
  trigger: 'review_approved',
  target: 'Done',
  ...over,
});

describe('reorder', () => {
  const list = ['a', 'b', 'c', 'd'];

  it('moves an item to a slot between items, the way the drop line shows it', () => {
    expect(reorder(list, 0, 2)).toEqual(['b', 'a', 'c', 'd']);
    expect(reorder(list, 3, 0)).toEqual(['d', 'a', 'b', 'c']);
    expect(reorder(list, 1, 4)).toEqual(['a', 'c', 'd', 'b']);
    expect(reorder(list, 2, 1)).toEqual(['a', 'c', 'b', 'd']);
  });

  it('answers the same array when the item would stay where it is', () => {
    expect(reorder(list, 1, 1)).toBe(list);
    expect(reorder(list, 1, 2)).toBe(list);
    expect(reorder(list, 0, 0)).toBe(list);
    expect(reorder(list, 3, 4)).toBe(list);
  });

  it('ignores a position that is not in the list and never changes its input', () => {
    expect(reorder(list, -1, 2)).toBe(list);
    expect(reorder(list, 4, 0)).toBe(list);
    reorder(list, 0, 3);
    expect(list).toEqual(['a', 'b', 'c', 'd']);
  });
});

describe('the drop line', () => {
  const rects = [
    { top: 0, bottom: 60 },
    { top: 72, bottom: 132 },
    { top: 144, bottom: 204 },
  ];

  it('drops before the first card whose middle is below the pointer, else at the end', () => {
    expect(dropSlot(10, rects)).toBe(0);
    expect(dropSlot(30, rects)).toBe(1);
    expect(dropSlot(100, rects)).toBe(1);
    expect(dropSlot(110, rects)).toBe(2);
    expect(dropSlot(300, rects)).toBe(3);
    expect(dropSlot(5, [])).toBe(0);
  });

  it('is drawn 6px outside the ends and between two cards in the middle', () => {
    expect(dropLineY(0, rects)).toBe(-6);
    expect(dropLineY(1, rects)).toBe(66);
    expect(dropLineY(2, rects)).toBe(138);
    expect(dropLineY(3, rects)).toBe(210);
    expect(dropLineY(9, rects)).toBe(210);
    expect(dropLineY(0, [])).toBe(0);
  });
});

describe('what changed in a rule', () => {
  const a = rule('a');

  it('does not count the name, which the server does not count either', () => {
    expect(ruleChanged(a, { ...a, name: 'Other' })).toBe(false);
    expect(sameRule(a, { ...a, name: 'Other' })).toBe(false);
    expect(sameRule(a, { ...a })).toBe(true);
  });

  it('counts the enabled flag, trigger, hours, target, mode and every filter', () => {
    const changes: Partial<SyncRule>[] = [
      { enabled: false },
      { trigger: 'merged' },
      { hours: 5 },
      { target: 'Closed' },
      { mode: 'any' },
      { filters: { ...a.filters, repo: 'acme/*' } },
      { filters: { ...a.filters, branch: 'main' } },
      { filters: { ...a.filters, project: 'INP' } },
      { filters: { ...a.filters, issueType: 'Bug' } },
      { filters: { ...a.filters, status: 'To Do' } },
    ];
    for (const c of changes) expect(ruleChanged(a, { ...a, ...c })).toBe(true);
  });

  it('keys a preview by everything but the name', () => {
    expect(previewKey(a)).toBe(previewKey({ ...a, name: 'Other', id: 'z' }));
    expect(previewKey(a)).not.toBe(previewKey({ ...a, target: 'Closed' }));
  });
});

describe('templates', () => {
  it('has one for every trigger, the two approval triggers included, and a blank rule', () => {
    const triggers = new Set(RULE_TEMPLATES.map((t) => t.rule.trigger));
    for (const t of TRIGGERS) expect(triggers.has(t.value)).toBe(true);
    expect(triggers.has('no_review_yet')).toBe(true);
    expect(triggers.has('waiting_approvals')).toBe(true);
    expect(RULE_TEMPLATES.at(-1)?.label).toBe('Blank rule');
    expect(new Set(RULE_TEMPLATES.map((t) => t.label)).size).toBe(RULE_TEMPLATES.length);
  });

  it('builds an enabled rule in mode All with no filters, and hours for a stale commit', () => {
    const stale = RULE_TEMPLATES.find((t) => t.rule.trigger === 'commit_stale');
    if (!stale) throw new Error('no stale template');
    const r = ruleFromTemplate(stale, [], 'x');
    expect(r).toMatchObject({
      id: 'x',
      enabled: true,
      mode: 'all',
      trigger: 'commit_stale',
      hours: 72,
      target: 'To Do',
      filters: { repo: '', branch: '', project: '', issueType: '', status: '' },
    });
  });

  it('keeps the spelling Jira uses for the suggested status', () => {
    const t = RULE_TEMPLATES[0];
    if (!t) throw new Error('no template');
    expect(ruleFromTemplate(t, ['in-review', 'Done'], 'x').target).toBe('in-review');
  });

  it('leaves the target empty when Jira does not list the suggested status, and keeps it with no list', () => {
    const t = RULE_TEMPLATES[0];
    if (!t) throw new Error('no template');
    expect(ruleFromTemplate(t, ['Done', 'Blocked'], 'x').target).toBe('');
    expect(ruleFromTemplate(t, [], 'x').target).toBe('In Review');
  });

  it('gives each new rule its own id', () => {
    const t = RULE_TEMPLATES[0];
    if (!t) throw new Error('no template');
    expect(ruleFromTemplate(t, []).id).not.toBe(ruleFromTemplate(t, []).id);
  });
});

describe('previewTable', () => {
  const preview = (over: Partial<RulePreview>): RulePreview => ({
    moves: [],
    waiting: [],
    ...over,
  });

  it('moves a ticket by the first rule that would move it', () => {
    const first = rule('a', { name: 'First', target: 'Ready to Merge' });
    const second = rule('b', { name: 'Second', trigger: 'merged', target: 'Done' });
    const t = previewTable([
      {
        rule: first,
        preview: preview({
          moves: [{ ticketKey: 'INP-1', status: 'In Review', alreadyInTarget: false }],
        }),
      },
      {
        rule: second,
        preview: preview({
          moves: [
            { ticketKey: 'INP-1', status: 'In Review', alreadyInTarget: false },
            { ticketKey: 'INP-2', status: 'To Do', alreadyInTarget: false },
          ],
        }),
      },
    ]);
    expect(t.rows).toEqual([
      {
        ticket: 'INP-1',
        from: 'In Review',
        when: 'The pull request is approved',
        to: 'Ready to Merge',
        because: 'First',
      },
      {
        ticket: 'INP-2',
        from: 'To Do',
        when: 'The pull request is merged',
        to: 'Done',
        because: 'Second',
      },
    ]);
    expect(t.stays).toEqual([]);
  });

  it('lists the tickets already in the target and the ones that wait under "won\'t move"', () => {
    const r = rule('a', { target: 'Done' });
    const t = previewTable([
      {
        rule: r,
        preview: preview({
          moves: [{ ticketKey: 'INP-2', status: 'Done', alreadyInTarget: true }],
          waiting: [
            {
              ticketKey: 'INP-3',
              total: 3,
              waitingOn: [
                { prId: 'p1', number: 34, repo: 'acme/fe' },
                { prId: 'p2', number: 56, repo: 'acme/proxy' },
              ],
            },
          ],
        }),
      },
    ]);
    expect(t.rows).toEqual([]);
    expect(t.stays).toEqual([
      { ticket: 'INP-2', from: 'Done', why: 'already in Done' },
      { ticket: 'INP-3', why: 'waiting on 2 of 3 PRs: fe #34, proxy #56' },
    ]);
  });

  it('does not list a moved ticket again, and lists a ticket once', () => {
    const a = rule('a', { target: 'Done' });
    const b = rule('b', { target: 'Closed' });
    const t = previewTable([
      {
        rule: a,
        preview: preview({
          moves: [
            { ticketKey: 'INP-1', status: 'To Do', alreadyInTarget: false },
            { ticketKey: 'INP-2', status: 'Done', alreadyInTarget: true },
          ],
        }),
      },
      {
        rule: b,
        preview: preview({
          moves: [
            { ticketKey: 'INP-2', status: 'Done', alreadyInTarget: true },
            { ticketKey: 'INP-1', status: 'To Do', alreadyInTarget: true },
          ],
          waiting: [{ ticketKey: 'INP-2', total: 2, waitingOn: [] }],
        }),
      },
    ]);
    expect(t.rows.map((r) => r.ticket)).toEqual(['INP-1']);
    expect(t.stays.map((s) => s.ticket)).toEqual(['INP-2']);
  });

  it('is empty without rules', () => {
    expect(previewTable([])).toEqual({ rows: [], stays: [] });
  });
});

describe('mergeTable', () => {
  const plan = (rule: string, target: string, waitingOn: number[] = []) => ({
    rule,
    target,
    total: waitingOn.length + 1,
    waitingOn: waitingOn.map((n) => ({ prId: `p${n}`, number: n, repo: 'acme/fe' })),
  });

  it('lists the tickets a merge would move, and the ones that wait for other pull requests', () => {
    const snap = snapshot([
      pr('a', 1, {
        tickets: [
          ticket('INP-1', { status: 'In Review', mergePlan: plan('Merged', 'Done') }),
          ticket('INP-2', { status: 'In Review', mergePlan: plan('Merged', 'Done', [9]) }),
          ticket('INP-3', { status: 'Done', mergePlan: plan('Merged', 'Done') }),
          ticket('INP-4'),
        ],
      }),
    ]);
    const t = mergeTable(snap);
    expect(t.rows).toEqual([
      {
        ticket: 'INP-1',
        from: 'In Review',
        when: 'The pull request is merged',
        to: 'Done',
        because: 'Merged',
      },
    ]);
    expect(t.stays).toEqual([
      { ticket: 'INP-2', from: 'In Review', why: '1 of 2 PRs have to merge first: fe #9' },
      { ticket: 'INP-3', from: 'Done', why: 'already in Done' },
    ]);
  });

  it('moves a ticket when merging any one of its pull requests leaves nothing to wait for', () => {
    const snap = snapshot([
      pr('a', 1, {
        tickets: [ticket('INP-1', { mergePlan: plan('Merged', 'Done', [2]) })],
      }),
      pr('b', 2, { tickets: [ticket('INP-1', { mergePlan: plan('Merged', 'Done') })] }),
    ]);
    expect(mergeTable(snap).rows.map((r) => r.ticket)).toEqual(['INP-1']);
    expect(mergeTable(snap).stays).toEqual([]);
  });

  it('is empty without a snapshot', () => {
    expect(mergeTable(undefined)).toEqual({ rows: [], stays: [] });
  });
});
