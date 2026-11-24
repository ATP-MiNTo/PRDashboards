import { memo } from 'react';
import type { SnapshotPr } from '@/api/client';
import { useSnapshotPr } from '@/api/queries';
import { blockOf, githubPrUrl, isStale } from '@/lib/snapshot';
import { CiCell } from './CiCell';
import styles from './CommandTable.module.css';
import { Pill, type Tone } from './Pill';
import { RelativeTime } from './RelativeTime';
import { TicketCell } from './TicketCell';
import { useNow } from './useNow';

const DECISION: Record<NonNullable<SnapshotPr['reviewDecision']>, { tone: Tone; label: string }> = {
  APPROVED: { tone: 'good', label: 'Approved' },
  CHANGES_REQUESTED: { tone: 'bad', label: 'Changes requested' },
  REVIEW_REQUIRED: { tone: 'warn', label: 'Review required' },
};

function ReviewCell({ pr }: { pr: SnapshotPr }) {
  const d = pr.reviewDecision
    ? DECISION[pr.reviewDecision]
    : { tone: 'mute' as const, label: 'No review' };
  const authors = pr.reviews.map((r) => r.author).filter((a): a is string => a !== null);
  return (
    <div className={styles.review}>
      <Pill tone={d.tone}>{d.label}</Pill>
      {authors.length > 0 && <span className={styles.muted}>{authors.join(', ')}</span>}
      {pr.reviewRequests.length > 0 && (
        <span className={styles.muted}>{pr.reviewRequests.length} requested</span>
      )}
    </div>
  );
}

function Commit({ pr }: { pr: SnapshotPr }) {
  const now = useNow(60_000);
  return (
    <div>
      <RelativeTime iso={pr.committedAt} />
      {isStale(pr, now) && (
        <>
          {' '}
          <Pill tone="warn">Stale</Pill>
        </>
      )}
    </div>
  );
}

function RowView({ id }: { id: string }) {
  const pr = useSnapshotPr(id);
  if (!pr) return null;
  const url = githubPrUrl(pr.repo.nameWithOwner, pr.number);
  const block = blockOf(pr);
  const child = pr.stack !== null && pr.stack.parentId !== null;
  return (
    <tr className={child ? `${styles.row} ${styles.child}` : styles.row} data-pr={pr.id}>
      <td className={styles.prCell}>
        <div className={styles.repo}>
          {pr.repo.nameWithOwner}
          {pr.repo.isNew && <Pill tone="info">New repo</Pill>}
        </div>
        <div className={styles.prTitle}>
          {url ? (
            <a href={url} target="_blank" rel="noopener noreferrer">
              {pr.title}
            </a>
          ) : (
            pr.title
          )}{' '}
          <span className={styles.muted}>#{pr.number}</span>
          {pr.isDraft && (
            <>
              {' '}
              <Pill tone="mute">Draft</Pill>
            </>
          )}
        </div>
        <div className={styles.branches}>
          {pr.headRef} → {pr.baseRef}
        </div>
      </td>
      <td>
        <TicketCell pr={pr} />
      </td>
      <td>
        <Commit pr={pr} />
      </td>
      <td>
        <ReviewCell pr={pr} />
      </td>
      <td>
        <CiCell pr={pr} />
      </td>
      <td>
        <span className={styles.muted} title="Merge methods are configured in a later release">
          Default
        </span>
      </td>
      <td>
        <button
          type="button"
          className={styles.mergeButton}
          disabled
          title="Merging arrives in a later release"
        >
          Merge
        </button>
        <div className={block.reason ? styles.reason : styles.ready}>
          {block.reason ?? 'Ready to merge'}
        </div>
      </td>
    </tr>
  );
}

export const Row = memo(RowView);
