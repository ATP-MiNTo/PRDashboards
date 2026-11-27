import type { Snapshot, SnapshotPr } from '@/api/client';

/**
 * Why a pull request is listed: the user is an assignee, opened it, or is asked to review it
 * (`reviewer`).
 */
export type Role = SnapshotPr['roles'][number];

/**
 * Which pull requests the list shows by the role the user has in them: `all` of them, `mine`
 * (assigned to the user or opened by the user, the green mode) or `review` (waiting on the user's
 * review, the red mode).
 */
export type ShowMode = 'all' | 'mine' | 'review';

export const SHOW_MODES: ShowMode[] = ['all', 'mine', 'review'];

export function isShowMode(v: unknown): v is ShowMode {
  return v === 'all' || v === 'mine' || v === 'review';
}

/**
 * The mode a stored value stands for. The values of the earlier Show switch map to the modes that
 * replaced them (everything is all, assigned and authored are mine); anything else is all.
 */
export function parseShowMode(v: unknown): ShowMode {
  if (isShowMode(v)) return v;
  return v === 'assigned' || v === 'authored' ? 'mine' : 'all';
}

const NO_REPOS: ReadonlySet<string> = new Set();

/**
 * The roles of a pull request. A snapshot without any (an older one) counts as assigned and
 * opened by the user, so it lists under All and Mine but never under To review.
 */
export function rolesOf(pr: Pick<SnapshotPr, 'roles'>): readonly Role[] {
  const roles = pr.roles as readonly Role[] | undefined;
  return roles && roles.length > 0 ? roles : ['assigned', 'authored'];
}

export type RoleKind = 'assigned' | 'reviewer' | 'both';

/**
 * The single cue a row gets: `assigned` (the green tint) for a pull request assigned to the user or
 * opened by the user, `reviewer` (the red tint) for one that is only waiting on the user's review,
 * and `both` (green and red) for one that is that and something else too.
 */
export function roleKind(pr: Pick<SnapshotPr, 'roles'>): RoleKind {
  const roles = rolesOf(pr);
  const r = roles.includes('reviewer');
  const mine = roles.includes('assigned') || roles.includes('authored');
  return r && mine ? 'both' : r ? 'reviewer' : 'assigned';
}

/** Whether a pull request is assigned to the user or opened by the user. */
const isMine = (pr: Pick<SnapshotPr, 'roles'>) => {
  const roles = rolesOf(pr);
  return roles.includes('assigned') || roles.includes('authored');
};

/** Whether a pull request shows in a mode. */
export function inShow(pr: Pick<SnapshotPr, 'roles'>, mode: ShowMode): boolean {
  if (mode === 'all') return true;
  return mode === 'mine' ? isMine(pr) : rolesOf(pr).includes('reviewer');
}

/** How many pull requests of the repositories that are not switched off (`off`) each mode shows. */
export function showCounts(
  s: Snapshot,
  off: ReadonlySet<string> = NO_REPOS,
): Record<ShowMode, number> {
  const out: Record<ShowMode, number> = { all: 0, mine: 0, review: 0 };
  for (const pr of s.prs) {
    if (off.has(pr.repo.id)) continue;
    out.all += 1;
    if (inShow(pr, 'mine')) out.mine += 1;
    if (inShow(pr, 'review')) out.review += 1;
  }
  return out;
}
