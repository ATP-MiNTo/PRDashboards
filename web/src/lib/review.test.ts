import { describe, expect, it } from 'vitest';
import { pr } from '@/test-fixtures';
import { myReview, reviewAccess, reviewBadge } from './review';

const review = (author: string, state: string, at: string) => ({
  id: `${author}${at}`,
  state,
  submittedAt: at,
  author,
});

describe('reviewBadge', () => {
  it.each([
    [{ have: 0, need: 1, changesRequested: 0 }, 'wait', 'Waiting'],
    [{ have: 0, need: 2, changesRequested: 0 }, 'wait', 'Waiting 0/2'],
    [{ have: 1, need: 2, changesRequested: 0 }, 'ok', 'Approve 1/2'],
    [{ have: 1, need: 1, changesRequested: 0 }, 'ok', 'Approve'],
    [{ have: 2, need: 2, changesRequested: 0 }, 'ok', 'Approve'],
    [{ have: 0, need: 2, changesRequested: 1 }, 'change', 'Change 1/2'],
    [{ have: 1, need: 1, changesRequested: 1 }, 'change', 'Change'],
  ] as const)('%j is %s, "%s"', (approvals, tone, label) => {
    expect(reviewBadge(approvals)).toMatchObject({ tone, label });
  });

  it('words the tooltip with the count and the rule of the repository', () => {
    expect(reviewBadge({ have: 1, need: 2, changesRequested: 0 }).tip).toBe(
      '1 of 2 reviewers approved. This repo needs 2 approvals to merge.',
    );
    expect(reviewBadge({ have: 0, need: 1, changesRequested: 0 }).tip).toBe(
      'Waiting on 1 reviewer. This repo needs 1 approval to merge.',
    );
    expect(reviewBadge({ have: 2, need: 2, changesRequested: 0 }).tip).toBe(
      'All 2 reviewers approved. This repo needs 2 approvals to merge.',
    );
    expect(reviewBadge({ have: 0, need: 2, changesRequested: 1 }).tip).toBe(
      '1 of 2 requested changes. This repo needs 2 approvals to merge.',
    );
  });

  it.each([
    [{ have: 0, need: 0, changesRequested: 0 }, 'wait', 'Waiting', 'No review yet.'],
    [{ have: 1, need: 0, changesRequested: 0 }, 'ok', 'Approve', '1 reviewer approved.'],
    [{ have: 3, need: 0, changesRequested: 0 }, 'ok', 'Approve', '3 reviewers approved.'],
    [
      { have: 1, need: 0, changesRequested: 1 },
      'change',
      'Change',
      '1 reviewer requested changes.',
    ],
  ] as const)(
    '%j, with nothing required, says what the reviewers did and no rule',
    (approvals, tone, label, tip) => {
      const badge = reviewBadge(approvals);
      expect(badge).toEqual({ tone, label, tip });
      expect(`${badge.label} ${badge.tip}`).not.toMatch(/needs|\/0|0 approval/i);
    },
  );

  it('starts the tooltip with what the viewer did', () => {
    expect(reviewBadge({ have: 1, need: 1, changesRequested: 0 }, 'approved').tip).toMatch(
      /^You approved\. /,
    );
    expect(reviewBadge({ have: 0, need: 1, changesRequested: 1 }, 'changes').tip).toMatch(
      /^You requested changes\. /,
    );
  });
});

describe('myReview', () => {
  it('is null without a login or a review of the viewer', () => {
    const row = pr('a', 1, { reviews: [review('bob', 'APPROVED', '2026-01-01T00:00:00Z')] });
    expect(myReview(row, undefined)).toBeNull();
    expect(myReview(row, 'octocat')).toBeNull();
  });

  it('takes the latest approval or request for changes, and ignores comments', () => {
    const row = pr('a', 1, {
      reviews: [
        review('Octocat', 'CHANGES_REQUESTED', '2026-01-01T00:00:00Z'),
        review('octocat', 'APPROVED', '2026-01-02T00:00:00Z'),
        review('octocat', 'COMMENTED', '2026-01-03T00:00:00Z'),
      ],
    });
    expect(myReview(row, 'octocat')).toBe('approved');
    expect(
      myReview(
        {
          reviews: [...row.reviews, review('octocat', 'CHANGES_REQUESTED', '2026-01-04T00:00:00Z')],
        },
        'octocat',
      ),
    ).toBe('changes');
  });
});

describe('reviewAccess', () => {
  it("allows a review of someone else's open pull request", () => {
    expect(reviewAccess(pr('a', 1, { roles: ['assigned'], author: 'bob' }), 'octocat')).toEqual({
      allowed: true,
      reason: null,
    });
  });

  it('refuses the pull requests the viewer opened, with a reason', () => {
    const own = reviewAccess(pr('a', 1, { roles: ['authored'] }), 'octocat');
    expect(own.allowed).toBe(false);
    expect(own.reason).toBe('You opened this pull request, so you cannot review it.');
    expect(
      reviewAccess(pr('a', 1, { roles: ['assigned'], author: 'OctoCat' }), 'octocat').allowed,
    ).toBe(false);
  });

  it('refuses a draft, with a reason', () => {
    expect(
      reviewAccess(pr('a', 1, { roles: ['assigned'], author: 'bob', isDraft: true }), 'octocat'),
    ).toEqual({
      allowed: false,
      reason: 'A draft is reviewed after it is marked ready.',
    });
  });

  it('shows the state instead of the menu once the viewer has reviewed', () => {
    const row = pr('a', 1, {
      roles: ['assigned'],
      author: 'bob',
      reviews: [review('octocat', 'APPROVED', '2026-01-01T00:00:00Z')],
    });
    expect(reviewAccess(row, 'octocat')).toEqual({ allowed: false, reason: null });
  });
});
