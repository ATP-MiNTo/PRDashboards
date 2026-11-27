import { memo, useState } from 'react';
import type { MergeMethodName, SnapshotPr } from '@/api/client';
import { useSnapshot, useSnapshotPr } from '@/api/queries';
import { ageShort, initials } from '@/lib/card';
import { effectiveMethod } from '@/lib/merge';
import { roleKind } from '@/lib/show';
import { blockOf, githubPrUrl, lastGoodGithub } from '@/lib/snapshot';
import { Marquee } from '../Marquee/Marquee';
import { CiCell } from './CiCell';
import styles from './CommandTable.module.css';
import { CommitIcon, LayersIcon, PrIcon } from './Icons';
import { MergeIcon } from './MergeIcon';
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

/** Lines added and removed, in the card's tabular style; nothing when the snapshot has none. */
function Diff({ pr }: { pr: SnapshotPr }) {
  if (pr.additions === null || pr.deletions === null) return null;
  return (
    <span className={styles.diff} title={`${pr.additions} lines added, ${pr.deletions} removed`}>
      <span className={styles.add}>+{pr.additions}</span>{' '}
      <span className={styles.del}>
        {'−'}
        {pr.deletions}
      </span>
    </span>
  );
}

/** "repo #num · author · updated age · latest commit age", then the badges that belong to it. */
function Meta({ pr }: { pr: SnapshotPr }) {
  const now = useNow(60_000);
  const lastGood = useSnapshot(lastGoodGithub).data ?? null;
  const updated = ageShort(pr.updatedAt, now);
  const commit = ageShort(pr.committedAt, now);
  return (
    <div className={styles.meta}>
      <span className={styles.metaText}>
        {pr.repo.nameWithOwner} #{pr.number}
      </span>
      {pr.author !== null && (
        <span className={styles.metaText}>
          {'·'} {pr.author}
        </span>
      )}
      {updated !== '' && (
        <span className={styles.metaIcon} title={`PR updated ${updated} ago`}>
          {'·'} <PrIcon />
          {updated}
        </span>
      )}
      {commit !== '' && (
        <span className={styles.metaIcon} title={`Latest commit ${commit} ago`}>
          {'·'} <CommitIcon />
          {commit}
        </span>
      )}
      {pr.repo.isNew && <Pill tone="info">New repo</Pill>}
      {lastGood !== null && (
        <Pill tone="warn">
          Last data <RelativeTime iso={lastGood.syncedAt} />
        </Pill>
      )}
      {pr.isDraft && <span className={styles.draftTag}>Draft</span>}
      {pr.stack && (
        <span className={styles.stackTag}>
          <LayersIcon />
          Stack {pr.stack.position}/{pr.stack.size}
        </span>
      )}
    </div>
  );
}

/** How a card leaves or arrives: fading out before it goes, or once when it is hidden or shown again. */
export type Motion = 'out' | 'in' | 'dim' | null;

interface CardProps {
  id: string;
  /** Whether the "Hide" box is checked. */
  hidden: boolean;
  /** Hidden cards shown among the others in the "All" mode. */
  dim: boolean;
  motion: Motion;
  onHide: (id: string) => void;
}

const MOTION_CLASS = { out: styles.ghostOut, in: styles.ghostIn, dim: styles.ghostDim } as const;

function CardView({ id, hidden, dim, motion, onHide }: CardProps) {
  const pr = useSnapshotPr(id);
  const [chosen, setChosen] = useState<MergeMethodName | null>(null);
  const [panel, setPanel] = useState(false);
  if (!pr) return null;
  const method = effectiveMethod(pr.mergeMethod, chosen);
  const url = githubPrUrl(pr.repo.nameWithOwner, pr.number);
  const block = blockOf(pr);
  const cls = [styles.card, styles[`role_${roleKind(pr)}`]];
  if (dim) cls.push(styles.dim);
  if (motion) cls.push(MOTION_CLASS[motion]);
  return (
    <article className={cls.join(' ')} data-pr={pr.id}>
      <div className={styles.cardMain}>
        <span className={styles.avatar} aria-hidden="true">
          {initials(pr.author)}
        </span>
        <div className={styles.cardBody}>
          <h2 className={styles.title}>
            <Marquee className={styles.titleMq}>
              {url ? (
                <a href={url} target="_blank" rel="noopener noreferrer">
                  {pr.title}
                </a>
              ) : (
                pr.title
              )}
            </Marquee>
          </h2>
          <Meta pr={pr} />
          <div className={styles.jira}>
            <TicketCell pr={pr} />
            <Diff pr={pr} />
          </div>
        </div>
      </div>
      <div className={styles.controls}>
        <label className={styles.hide}>
          <input
            type="checkbox"
            className={styles.hideBox}
            checked={hidden}
            aria-label={`Hide #${pr.number}`}
            onChange={() => onHide(pr.id)}
          />
          <span className={styles.hideText}>Hide</span>
        </label>
        <span className={styles.vline} aria-hidden="true" />
        <div className={styles.cell}>
          <ReviewCell pr={pr} />
        </div>
        <span className={styles.vline} aria-hidden="true" />
        <div className={styles.cell}>
          <CiCell pr={pr} />
        </div>
        <span className={styles.vline} aria-hidden="true" />
        <div className={styles.cell}>
          <MethodCell resolution={pr.mergeMethod} value={method} onChange={setChosen} />
        </div>
        <span className={styles.vline} aria-hidden="true" />
        <div className={styles.cell}>
          <div className={styles.mergeCell}>
            <button
              type="button"
              className={styles.mergeButton}
              disabled={block.reason !== null}
              aria-expanded={panel}
              onClick={() => setPanel((open) => !open)}
            >
              Merge
              <MergeIcon />
            </button>
            <div className={block.reason ? styles.reason : styles.ready}>
              {block.reason ?? 'Ready to merge'}
            </div>
          </div>
        </div>
      </div>
      {panel && (
        <div className={styles.panel} data-panel={pr.id}>
          <MergePanel pr={pr} method={method} onClose={() => setPanel(false)} />
        </div>
      )}
    </article>
  );
}

export const Card = memo(CardView);
