import type { MergeMethodName, MethodResolution, Transition } from '@/api/client';
import type { DropdownOption } from '@/components/Dropdown/Dropdown';

export const METHOD_NAMES: Record<MergeMethodName, string> = {
  squash: 'Squash',
  merge: 'Merge commit',
  rebase: 'Rebase',
};

const ORDER: MergeMethodName[] = ['squash', 'merge', 'rebase'];

/** Plain method choices, for settings that have no repository to disallow one. */
export const METHOD_CHOICES: DropdownOption<MergeMethodName>[] = ORDER.map((value) => ({
  value,
  label: METHOD_NAMES[value],
}));

/** The options of a method dropdown: the resolved method is the default, the rest are not allowed. */
export function methodOptions(r: MethodResolution): DropdownOption<MergeMethodName>[] {
  return ORDER.map((m) => {
    const allowed = r.allowed.includes(m);
    const note = m === r.method ? ' (default)' : allowed ? '' : ' (not allowed)';
    return { value: m, label: METHOD_NAMES[m] + note, disabled: !allowed };
  });
}

/** The method to merge with: the person's choice if the repository still allows it. */
export function effectiveMethod(
  r: MethodResolution,
  chosen: MergeMethodName | null,
): MergeMethodName {
  return chosen && r.allowed.includes(chosen) ? chosen : r.method;
}

/** How the resolved default came about, in words for the hint under the dropdown. */
export function sourceHint(source: string): string {
  return source.startsWith('rule: ')
    ? `Branch rule ${source.slice(6)}`
    : source.slice(0, 1).toUpperCase() + source.slice(1);
}

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
