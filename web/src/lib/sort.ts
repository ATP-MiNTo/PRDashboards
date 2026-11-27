import type { SnapshotPr } from '@/api/client';
import { blockOf, type Group } from './snapshot';

export type SortKey = 'updated' | 'title' | 'repo' | 'size' | 'ready';

export const SORT_OPTIONS: { value: SortKey; label: string }[] = [
  { value: 'updated', label: 'Recently updated' },
  { value: 'title', label: 'Title' },
  { value: 'repo', label: 'Repository' },
  { value: 'size', label: 'Size of change' },
  { value: 'ready', label: 'Ready to merge first' },
];

export const DEFAULT_SORT: SortKey = 'updated';

const READINESS = { ready: 0, blocked: 1, draft: 2 } as const;

/** When the pull request last changed; a snapshot without the update time falls back to the last commit. */
function time(pr: SnapshotPr): number {
  const iso = pr.updatedAt ?? pr.committedAt;
  const t = iso ? Date.parse(iso) : NaN;
  return Number.isNaN(t) ? -Infinity : t;
}

/** Lines added plus removed; 0 when the snapshot does not say. */
export function sizeOf(pr: Pick<SnapshotPr, 'additions' | 'deletions'>): number {
  return (pr.additions ?? 0) + (pr.deletions ?? 0);
}

function text(a: string, b: string): number {
  return a.localeCompare(b, undefined, { sensitivity: 'base', numeric: true });
}

/** Compares in the option's natural order: negative when `a` comes first. */
function compare(key: SortKey, a: SnapshotPr, b: SnapshotPr): number {
  switch (key) {
    case 'updated':
      // Newest first. Infinities do not subtract, so compare them.
      return time(a) === time(b) ? 0 : time(a) > time(b) ? -1 : 1;
    case 'title':
      return text(a.title, b.title);
    case 'repo':
      return text(a.repo.nameWithOwner, b.repo.nameWithOwner) || a.number - b.number;
    case 'size':
      // Biggest change first.
      return sizeOf(b) - sizeOf(a);
    case 'ready':
      return READINESS[blockOf(a).readiness] - READINESS[blockOf(b).readiness];
  }
}

/**
 * Sorts groups by the key of their best member, so a stack stays one unit with its members in stack
 * order. Equal keys keep the incoming order. `reversed` flips the natural direction.
 */
export function sortGroups(
  groups: Group[],
  prs: ReadonlyMap<string, SnapshotPr>,
  key: SortKey,
  reversed: boolean,
): Group[] {
  const sign = reversed ? -1 : 1;
  const cmp = (a: SnapshotPr, b: SnapshotPr) => sign * compare(key, a, b);
  const best = new Map<string, SnapshotPr>();
  for (const g of groups) {
    const members = g.ids.map((id) => prs.get(id)).filter((p): p is SnapshotPr => p !== undefined);
    const first = members.reduce<SnapshotPr | undefined>(
      (acc, p) => (acc === undefined || cmp(p, acc) < 0 ? p : acc),
      undefined,
    );
    if (first) best.set(g.key, first);
  }
  return groups
    .map((g, i) => ({ g, i }))
    .sort((x, y) => {
      const a = best.get(x.g.key);
      const b = best.get(y.g.key);
      if (a && b) return cmp(a, b) || x.i - y.i;
      return x.i - y.i;
    })
    .map((e) => e.g);
}
