import type { Approvals, SnapshotPr } from '@/api/client';
import { rolesOf } from './show';

/** What the viewer already did on a pull request, from the reviews the snapshot carries. */
export type MyReview = 'approved' | 'changes' | null;

export type ReviewTone = 'ok' | 'wait' | 'change';

export interface ReviewBadge {
  tone: ReviewTone;
  /** "Approve", "Approve 1/2", "Waiting 0/2" or "Change 1/2": the fraction shows when more than one approval is needed. */
  label: string;
  /** The tooltip: who approved, then the rule of the repository. */
  tip: string;
}

const plural = (n: number, word: string) => `${word}${n === 1 ? '' : 's'}`;

/**
 * The latest review of the viewer on a pull request that is an approval or a request for changes.
 * `login` is the connected GitHub login; without it nothing is known.
 */
export function myReview(pr: Pick<SnapshotPr, 'reviews'>, login: string | undefined): MyReview {
  if (!login) return null;
  const mine = pr.reviews
    .filter((r) => r.author?.toLowerCase() === login.toLowerCase())
    .filter((r) => r.state === 'APPROVED' || r.state === 'CHANGES_REQUESTED')
    .sort((a, b) => Date.parse(a.submittedAt ?? '') - Date.parse(b.submittedAt ?? ''))
    .at(-1);
  if (!mine) return null;
  return mine.state === 'APPROVED' ? 'approved' : 'changes';
}

/** The badge of the Review cell, from the counts the server computed. */
export function reviewBadge(a: Approvals, mine: MyReview = null): ReviewBadge {
  const need = a.need;
  const frac = need > 1;
  const you =
    mine === 'approved' ? 'You approved. ' : mine === 'changes' ? 'You requested changes. ' : '';
  const rule = `This repo needs ${need} ${plural(need, 'approval')} to merge.`;
  if (a.changesRequested > 0) {
    return {
      tone: 'change',
      label: frac ? `Change ${a.changesRequested}/${need}` : 'Change',
      tip: `${you}${a.changesRequested} of ${need} requested changes. ${rule}`,
    };
  }
  if (a.have >= need) {
    return {
      tone: 'ok',
      label: 'Approve',
      tip: `${you}All ${need} ${plural(need, 'reviewer')} approved. ${rule}`,
    };
  }
  if (a.have > 0) {
    return {
      tone: 'ok',
      label: `Approve ${a.have}/${need}`,
      tip: `${you}${a.have} of ${need} reviewers approved. ${rule}`,
    };
  }
  return {
    tone: 'wait',
    label: frac ? `Waiting 0/${need}` : 'Waiting',
    tip: `${you}Waiting on ${need} ${plural(need, 'reviewer')}. ${rule}`,
  };
}

export interface ReviewAccess {
  /** Whether the Action menu (Approve, Request change) is offered. */
  allowed: boolean;
  /** Why not, in a sentence for the tooltip. */
  reason: string | null;
}

/**
 * Whether the viewer can act on a pull request. A pull request the viewer opened cannot be reviewed
 * by them (GitHub refuses it, the server answers `own_pull_request`), a draft is not reviewed yet,
 * and one the viewer already approved or asked changes on shows its state instead.
 */
export function reviewAccess(
  pr: Pick<SnapshotPr, 'roles' | 'author' | 'isDraft' | 'reviews'>,
  login: string | undefined,
): ReviewAccess {
  const own =
    rolesOf(pr).includes('authored') ||
    (login !== undefined && pr.author?.toLowerCase() === login.toLowerCase());
  if (own)
    return { allowed: false, reason: 'You opened this pull request, so you cannot review it.' };
  if (pr.isDraft)
    return { allowed: false, reason: 'A draft is reviewed after it is marked ready.' };
  if (myReview(pr, login) !== null) return { allowed: false, reason: null };
  return { allowed: true, reason: null };
}
