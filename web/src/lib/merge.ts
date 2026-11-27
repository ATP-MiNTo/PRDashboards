import type { MergeMethodName, MergeStep, MethodResolution, Transition } from '@/api/client';
import type { DropdownOption } from '@/components/Dropdown/Dropdown';

export const METHOD_NAMES: Record<MergeMethodName, string> = {
  squash: 'Squash',
  merge: 'Merge commit',
  rebase: 'Rebase',
};

/** The methods in the order the dialog and the settings list them. */
export const METHOD_ORDER: readonly MergeMethodName[] = ['squash', 'merge', 'rebase'];

/** Plain method choices, for settings that have no repository to disallow one. */
export const METHOD_CHOICES: DropdownOption<MergeMethodName>[] = METHOD_ORDER.map((value) => ({
  value,
  label: METHOD_NAMES[value],
}));

/** The method to merge with: the person's choice if the repository still allows it. */
export function effectiveMethod(
  r: MethodResolution,
  chosen: MergeMethodName | null,
): MergeMethodName {
  return chosen && r.allowed.includes(chosen) ? chosen : r.method;
}

/**
 * Where the resolved method comes from, to follow "Method: squash, from ..." in the merge dialog:
 * a branch rule, the repository's settings or the defaults of a new repository.
 */
export function methodFrom(source: string): string {
  if (source.startsWith('rule: ')) return `branch rule ${source.slice(6)}`;
  if (source === 'repo default') return 'the repo settings';
  if (source === 'new repo default') return 'the defaults';
  return source.toLowerCase();
}

/** A method as the dialog's sentence says it: lower case. */
export const methodWord = (m: MergeMethodName): string => METHOD_NAMES[m].toLowerCase();

export interface TicketChoice {
  key: string;
  /** Null leaves the ticket where it is. */
  transition: Transition | null;
  fields: Record<string, string>;
  confirmed: boolean;
}

/** The label of the merge button, per the ticket moves chosen. */
export function mergeLabel(moves: TicketChoice[]): string {
  const moved = moves.filter((m) => m.transition);
  const first = moved[0]?.transition;
  if (!first) return 'Merge without ticket';
  return moved.length === 1
    ? `Merge & move to ${first.toStatus}`
    : `Merge & update ${moved.length} tickets`;
}

/** Why the merge cannot be sent yet, or null. */
export function mergeBlock(moves: TicketChoice[], withoutTicketConfirmed: boolean): string | null {
  const moved = moves.filter((m) => m.transition);
  if (moved.length === 0) {
    return withoutTicketConfirmed ? null : 'Confirm that no ticket will be moved';
  }
  for (const m of moved) {
    for (const f of m.transition?.fields ?? []) {
      if (f.required && !m.fields[f.id]?.trim()) return `${m.key}: ${f.name} is required`;
    }
    if (!m.confirmed) return `Confirm the move of ${m.key}`;
  }
  return null;
}

/** The first and last word of the merge toast: what happened to the pull request and its tickets. */
export interface MergeSummary {
  /** "#490 merged, INP-104 moved to Done". */
  text: string;
  /** Whether a step failed, so the toast is an error and says where to retry. */
  failed: boolean;
  /** Whether GitHub merged it. False when it was only added to the merge queue. */
  merged: boolean;
}

/** Sums up the steps of a finished merge for the toast. */
export function mergeSummary(number: number, steps: readonly MergeStep[]): MergeSummary {
  const merge = steps.find((s) => s.kind === 'merge');
  const queued = merge?.state === 'queued';
  const parts: string[] = [];
  let failed = false;
  for (const s of steps) {
    if (s.state === 'failed') {
      failed = true;
      if (s.kind === 'transition') parts.push(`${s.ticket ?? 'a ticket'} did not move`);
      else if (s.kind === 'delete_branch') parts.push('the branch was not deleted');
    } else if (s.kind === 'transition' && s.state === 'ok') {
      parts.push(`${s.ticket ?? 'a ticket'} moved${s.toStatus ? ` to ${s.toStatus}` : ''}`);
    }
  }
  const head = queued ? `#${number} added to the merge queue` : `#${number} merged`;
  const retry = failed ? '. Retry it from History' : '';
  return {
    text: `${head}${parts.length > 0 ? `, ${parts.join(', ')}` : ''}${retry}`,
    failed,
    merged: !queued,
  };
}
