import type { PrRef, RuleFilters, RulePreview, Snapshot, SyncRule } from '@/api/client';
import { emptyFilters, newRule, refText, ruleName, statusKey, triggerLabel } from './rules';

/** The rules modal: the pure parts of reordering, templates, "what changed" and the preview table. */

// ----- reordering -----

/**
 * Moves the item at `from` to the slot `slot`, a position between items (0 before the first, `length`
 * after the last), the way a drop line shows it. Returns the same array when nothing would move.
 */
export function reorder<T>(list: readonly T[], from: number, slot: number): readonly T[] {
  if (from < 0 || from >= list.length) return list;
  const to = slot > from ? slot - 1 : slot;
  if (to === from || to < 0 || to > list.length - 1) return list;
  const next = [...list];
  const [item] = next.splice(from, 1);
  if (item === undefined) return list;
  next.splice(to, 0, item);
  return next;
}

/** The slot a pointer at `y` drops into: before the first card whose middle is below it, else the end. */
export function dropSlot(y: number, rects: readonly { top: number; bottom: number }[]): number {
  const at = rects.findIndex((r) => y < r.top + (r.bottom - r.top) / 2);
  return at < 0 ? rects.length : at;
}

/** Where the drop line is drawn for a slot, in the coordinates of `rects`: 6px outside the ends, between two cards in the middle. */
export function dropLineY(slot: number, rects: readonly { top: number; bottom: number }[]): number {
  const first = rects[0];
  const last = rects[rects.length - 1];
  if (!first || !last) return 0;
  if (slot <= 0) return first.top - 6;
  if (slot >= rects.length) return last.bottom + 6;
  const before = rects[slot - 1];
  const after = rects[slot];
  return before && after ? (before.bottom + after.top) / 2 : 0;
}

// ----- what changed -----

const sameFilters = (a: RuleFilters, b: RuleFilters) =>
  a.repo === b.repo &&
  a.branch === b.branch &&
  a.project === b.project &&
  a.issueType === b.issueType &&
  a.status === b.status;

/** Whether the server treats the rule as changed: it then loses what it fired before. The name does not count. */
export function ruleChanged(a: SyncRule, b: SyncRule): boolean {
  return (
    a.enabled !== b.enabled ||
    a.trigger !== b.trigger ||
    a.hours !== b.hours ||
    a.target !== b.target ||
    a.mode !== b.mode ||
    !sameFilters(a.filters, b.filters)
  );
}

/** Whether two rules are the same in every field, the name included. */
export const sameRule = (a: SyncRule, b: SyncRule): boolean =>
  a.name === b.name && !ruleChanged(a, b);

/** A key of what a preview depends on: the rule without its name. */
export const previewKey = (r: SyncRule): string =>
  JSON.stringify([r.enabled, r.trigger, r.hours, r.target, r.mode, r.filters]);

// ----- templates -----

export interface RuleTemplate {
  /** The text of the menu item. */
  label: string;
  rule: Pick<SyncRule, 'name' | 'trigger' | 'target'> & Partial<Pick<SyncRule, 'hours'>>;
}

/** The "Add rule" menu: one rule for each trigger, and a blank one. */
export const RULE_TEMPLATES: readonly RuleTemplate[] = [
  {
    label: 'Move to In Review when a PR is ready',
    rule: { name: 'Ready for review', trigger: 'ready_for_review', target: 'In Review' },
  },
  {
    label: 'Move to In Review on a review request',
    rule: { name: 'Review requested', trigger: 'review_requested', target: 'In Review' },
  },
  {
    label: 'Move to In Review with no review yet',
    rule: { name: 'No review yet', trigger: 'no_review_yet', target: 'In Review' },
  },
  {
    label: 'Move to In Review on a new review',
    rule: { name: 'Review submitted', trigger: 'review_submitted', target: 'In Review' },
  },
  {
    label: 'Move to In Review while approvals are missing',
    rule: { name: 'Waiting for approvals', trigger: 'waiting_approvals', target: 'In Review' },
  },
  {
    label: 'Move to Ready to Merge when approved',
    rule: { name: 'Approved', trigger: 'review_approved', target: 'Ready to Merge' },
  },
  {
    label: 'Move back to In Progress on changes requested',
    rule: { name: 'Changes requested', trigger: 'changes_requested', target: 'In Progress' },
  },
  {
    label: 'Move to In Progress when CI fails',
    rule: { name: 'CI failing', trigger: 'ci_failing', target: 'In Progress' },
  },
  {
    label: 'Move to In Review when CI passes',
    rule: { name: 'CI passing', trigger: 'ci_passing', target: 'In Review' },
  },
  {
    label: 'Move to To Do when a commit is 3 days old',
    rule: { name: 'Stale commit', trigger: 'commit_stale', target: 'To Do', hours: 72 },
  },
  {
    label: 'Move to Done when a PR merges',
    rule: { name: 'Merged', trigger: 'merged', target: 'Done' },
  },
  { label: 'Blank rule', rule: { name: 'New rule', trigger: 'merged', target: '' } },
];

/**
 * A new, enabled rule from a template. A target that Jira does not list (by `statusKey`) is left
 * empty so the person picks one; with no list at all the suggestion stays.
 */
export function ruleFromTemplate(
  t: RuleTemplate,
  statuses: readonly string[],
  id = newRule().id,
): SyncRule {
  const listed = statuses.find((s) => statusKey(s) === statusKey(t.rule.target));
  return {
    ...newRule(),
    id,
    name: t.rule.name,
    trigger: t.rule.trigger,
    hours: t.rule.hours ?? 0,
    target: listed ?? (statuses.length === 0 ? t.rule.target : ''),
    filters: emptyFilters(),
  };
}

// ----- the single-valued chip fields -----

/** The filters of a rule that are chips, with the label of the field and what the chip says. */
export type ChipFilter = 'branch' | 'repo' | 'project' | 'status' | 'issueType';

export const CHIP_LABELS: Record<ChipFilter, string> = {
  branch: 'On branches',
  repo: 'In repositories',
  project: 'In Jira projects',
  status: 'If the ticket is in',
  issueType: 'Of issue type',
};

// ----- the preview table -----

export interface PreviewRow {
  ticket: string;
  /** The status the ticket is in now. */
  from: string;
  /** What happens: the trigger of the rule, in words. */
  when: string;
  to: string;
  /** The rule that moves it. */
  because: string;
}

export interface PreviewStay {
  ticket: string;
  from?: string;
  why: string;
}

export interface PreviewTable {
  rows: PreviewRow[];
  stays: PreviewStay[];
}

const waitingText = (total: number, on: readonly PrRef[]) =>
  `waiting on ${on.length} of ${total} PRs: ${on.map(refText).join(', ')}`;

/**
 * Combines the previews of the enabled rules, in order, into one table. A ticket moves by the first
 * rule that would move it, as the engine does. A ticket no rule moves is listed with the reason of
 * the first rule that has one.
 */
export function previewTable(
  entries: readonly { rule: SyncRule; preview: RulePreview }[],
): PreviewTable {
  const rows: PreviewRow[] = [];
  const moved = new Set<string>();
  for (const { rule, preview } of entries) {
    for (const m of preview.moves) {
      if (m.alreadyInTarget || moved.has(m.ticketKey)) continue;
      moved.add(m.ticketKey);
      rows.push({
        ticket: m.ticketKey,
        from: m.status,
        when: triggerLabel(rule.trigger),
        to: rule.target,
        because: ruleName(rule),
      });
    }
  }
  const stays = new Map<string, PreviewStay>();
  for (const { rule, preview } of entries) {
    for (const m of preview.moves) {
      if (!m.alreadyInTarget || moved.has(m.ticketKey) || stays.has(m.ticketKey)) continue;
      stays.set(m.ticketKey, {
        ticket: m.ticketKey,
        from: m.status,
        why: `already in ${rule.target}`,
      });
    }
    for (const w of preview.waiting) {
      if (moved.has(w.ticketKey) || stays.has(w.ticketKey)) continue;
      stays.set(w.ticketKey, { ticket: w.ticketKey, why: waitingText(w.total, w.waitingOn) });
    }
  }
  return { rows, stays: [...stays.values()] };
}

/**
 * What the saved merged rules do when pull requests are merged, from the merge plans the snapshot
 * carries. A ticket moves when merging one of its pull requests leaves nothing to wait for.
 */
export function mergeTable(snapshot: Snapshot | undefined): PreviewTable {
  const rows: PreviewRow[] = [];
  const stays: PreviewStay[] = [];
  const seen = new Set<string>();
  const byTicket = new Map<
    string,
    {
      status: string;
      plans: NonNullable<Snapshot['prs'][number]['tickets'][number]['mergePlan']>[];
    }
  >();
  for (const pr of snapshot?.prs ?? []) {
    for (const t of pr.tickets) {
      if (!t.mergePlan) continue;
      const entry = byTicket.get(t.key) ?? { status: t.status ?? '', plans: [] };
      entry.plans.push(t.mergePlan);
      byTicket.set(t.key, entry);
    }
  }
  for (const [key, { status, plans }] of byTicket) {
    if (seen.has(key)) continue;
    seen.add(key);
    const ready = plans.find((p) => p.waitingOn.length === 0);
    const first = plans[0];
    if (!first) continue;
    if (ready && statusKey(ready.target) === statusKey(status)) {
      stays.push({ ticket: key, from: status, why: `already in ${ready.target}` });
    } else if (ready) {
      rows.push({
        ticket: key,
        from: status,
        when: triggerLabel('merged'),
        to: ready.target,
        because: ready.rule,
      });
    } else {
      stays.push({
        ticket: key,
        from: status,
        why: `${first.waitingOn.length} of ${first.total} PRs have to merge first: ${first.waitingOn.map(refText).join(', ')}`,
      });
    }
  }
  return { rows, stays };
}
