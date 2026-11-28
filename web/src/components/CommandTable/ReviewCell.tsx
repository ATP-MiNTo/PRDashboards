import { useId, useState } from 'react';
import { ApiError, type SnapshotPr } from '@/api/client';
import { useAccounts, useApprove } from '@/api/queries';
import { describeReviewProblem } from '@/lib/problems';
import { myReview, reviewAccess, reviewBadge, type ReviewBadge } from '@/lib/review';
import { MenuItem, PopMenu } from '../PopMenu/PopMenu';
import { Tip } from '../Tip/Tip';
import { useToast } from '../Toast/Toast';
import styles from './Controls.module.css';
import { AlertIcon, CheckIcon, ChevronDownIcon } from './Icons';
import { RequestChangesDialog } from './RequestChangesDialog';

const TONE: Record<ReviewBadge['tone'], string> = {
  ok: `${styles.badge} ${styles.ok}`,
  wait: `${styles.badge} ${styles.ghost}`,
  change: `${styles.badge} ${styles.err} ${styles.change}`,
};

/**
 * The Review cell. A pull request the viewer can review shows the "Action" menu (Approve, Request
 * change); every other one shows its state as a badge: "Approve 1/2", "Waiting 0/2", "Change 1/2".
 * The tooltip says how many reviewers approved and what the repository needs.
 */
export function ReviewCell({ pr }: { pr: SnapshotPr }) {
  const login = useAccounts().data?.github.login ?? undefined;
  const access = reviewAccess(pr, login);
  const badge = reviewBadge(pr.approvals, myReview(pr, login));
  const approve = useApprove();
  const toast = useToast();
  const describe = useId();
  const [changes, setChanges] = useState(false);
  const tip = access.reason ? `${badge.tip} ${access.reason}` : badge.tip;

  const doApprove = () =>
    approve.mutate(
      { id: pr.id, body: { expectedHeadSha: pr.headSha } },
      {
        onSuccess: () => toast.show(`You approved #${pr.number}`, 'success'),
        onError: (e) =>
          toast.show(
            describeReviewProblem(
              e instanceof ApiError ? e.code : undefined,
              e instanceof ApiError ? e.detail : undefined,
            ),
            'error',
          ),
      },
    );

  if (!access.allowed) {
    return (
      <div className={styles.badgeRow}>
        <Tip tip={tip}>
          <span className={TONE[badge.tone]}>
            {badge.label}
            <span className={styles.srOnly}>. {tip}</span>
          </span>
        </Tip>
      </div>
    );
  }

  return (
    <div className={styles.badgeRow}>
      <span id={describe} className={styles.srOnly}>
        {tip}
      </span>
      <PopMenu
        label={`Review action for #${pr.number}`}
        menuLabel={`Review #${pr.number}`}
        triggerClassName={`${styles.badge} ${styles.action}`}
        trigger={
          <>
            Action
            <ChevronDownIcon />
          </>
        }
        width="w44"
        side="auto"
        hover
        disabled={approve.isPending}
        describedBy={describe}
      >
        {(close) => (
          <>
            <MenuItem
              onSelect={() => {
                close();
                doApprove();
              }}
            >
              <span className={`${styles.menuIcon} ${styles.menuApprove}`}>
                <CheckIcon />
              </span>
              Approve
            </MenuItem>
            <MenuItem
              onSelect={() => {
                close();
                setChanges(true);
              }}
            >
              <span className={`${styles.menuIcon} ${styles.menuChange}`}>
                <AlertIcon />
              </span>
              Request changes
            </MenuItem>
          </>
        )}
      </PopMenu>
      {changes && <RequestChangesDialog pr={pr} onClose={() => setChanges(false)} />}
    </div>
  );
}
