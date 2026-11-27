import type { Snapshot, SnapshotPr } from '@/api/client';

/** Why a pull request is listed: the user is an assignee, or opened it. */
export type Role = SnapshotPr['roles'][number];

/** Which pull requests the list shows by the role the user has in them. */
export type ShowMode = 'everything' | 'assigned' | 'authored';

export const SHOW_MODES: ShowMode[] = ['everything', 'assigned', 'authored'];

export function isShowMode(v: unknown): v is ShowMode {
  return v === 'everything' || v === 'assigned' || v === 'authored';
}

const NO_REPOS: ReadonlySet<string> = new Set();

/** The roles of a pull request. A snapshot without any (an older one) counts for both. */
export function rolesOf(pr: Pick<SnapshotPr, 'roles'>): readonly Role[] {
  const roles = pr.roles as readonly Role[] | undefined;
  return roles && roles.length > 0 ? roles : ['assigned', 'authored'];
}

export type RoleKind = 'assigned' | 'authored' | 'both';

/** The single cue a row gets: one role, or both. */
export function roleKind(pr: Pick<SnapshotPr, 'roles'>): RoleKind {
  const roles = rolesOf(pr);
  const a = roles.includes('assigned');
  const o = roles.includes('authored');
  return a && o ? 'both' : a ? 'assigned' : 'authored';
}

/** Whether a pull request shows in a mode. */
export function inShow(pr: Pick<SnapshotPr, 'roles'>, mode: ShowMode): boolean {
  return mode === 'everything' || rolesOf(pr).includes(mode);
}

/** How many pull requests of the repositories that are not switched off (`off`) each mode shows. */
export function showCounts(
  s: Snapshot,
  off: ReadonlySet<string> = NO_REPOS,
): Record<ShowMode, number> {
  const out: Record<ShowMode, number> = { everything: 0, assigned: 0, authored: 0 };
  for (const pr of s.prs) {
    if (off.has(pr.repo.id)) continue;
    out.everything += 1;
    const roles = rolesOf(pr);
    if (roles.includes('assigned')) out.assigned += 1;
    if (roles.includes('authored')) out.authored += 1;
  }
  return out;
}
