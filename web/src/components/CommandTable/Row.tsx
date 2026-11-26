import { memo, useState } from 'react';
import type { MergeMethodName, SnapshotPr } from '@/api/client';
import { useSnapshot, useSnapshotPr } from '@/api/queries';
import { effectiveMethod } from '@/lib/merge';
import { roleKind } from '@/lib/show';
import { blockOf, githubPrUrl, isStale, lastGoodGithub } from '@/lib/snapshot';
import { CiCell } from './CiCell';
import styles from './CommandTable.module.css';
import { MergePanel } from './MergePanel';
import { MethodCell } from './MethodCell';
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

interface RowProps {
  id: string;
  /** Whether the "Hide" box is checked. */
  hidden: boolean;
  /** Hidden rows shown among the others in the "All" mode. */
  dim: boolean;
  /** Fading out before it leaves the list. */
  leaving: boolean;
  onHide: (id: string) => void;
}

function RowView({ id, hidden, dim, leaving, onHide }: RowProps) {
  const pr = useSnapshotPr(id);
  const lastGood = useSnapshot(lastGoodGithub).data ?? null;
  const [chosen, setChosen] = useState<MergeMethodName | null>(null);
  const [panel, setPanel] = useState(false);
  if (!pr) return null;
  const method = effectiveMethod(pr.mergeMethod, chosen);
  const url = githubPrUrl(pr.repo.nameWithOwner, pr.number);
  const block = blockOf(pr);
  const child = pr.stack !== null && pr.stack.parentId !== null;
  const rowClass = [styles.row, styles[`role_${roleKind(pr)}`]];
  if (child) rowClass.push(styles.child);
  if (dim) rowClass.push(styles.dim);
  if (leaving) rowClass.push(styles.ghost);
  return (
    <>
      <tr className={rowClass.join(' ')} data-pr={pr.id}>
        <td className={styles.prCell}>
          <div className={styles.repo}>
            {pr.repo.nameWithOwner}
            {pr.repo.isNew && <Pill tone="info">New repo</Pill>}
            {lastGood !== null && (
              <Pill tone="warn">
                Last data <RelativeTime iso={lastGood.syncedAt} />
              </Pill>
            )}
            <label className={styles.hide}>
              <input
                type="checkbox"
                checked={hidden}
                aria-label={`Hide #${pr.number}`}
                onChange={() => onHide(pr.id)}
              />
              Hide
            </label>
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
          <MethodCell resolution={pr.mergeMethod} value={method} onChange={setChosen} />
        </td>
        <td>
          <button
            type="button"
            className={styles.mergeButton}
            disabled={block.reason !== null}
            aria-expanded={panel}
            onClick={() => setPanel((open) => !open)}
          >
            Merge
          </button>
          <div className={block.reason ? styles.reason : styles.ready}>
            {block.reason ?? 'Ready to merge'}
          </div>
        </td>
      </tr>
      {panel && (
        <tr className={styles.panelRow} data-panel={pr.id}>
          <td colSpan={7}>
            <MergePanel pr={pr} method={method} onClose={() => setPanel(false)} />
          </td>
        </tr>
      )}
    </>
  );
}

export const Row = memo(RowView);
