import { memo, useState } from 'react';
import type { MergeResult, SnapshotPr } from '@/api/client';
import { useSnapshot, useSnapshotPr } from '@/api/queries';
import { ageShort, initials } from '@/lib/card';
import { roleKind } from '@/lib/show';
import { githubPrUrl, lastGoodGithub } from '@/lib/snapshot';
import { Marquee } from '../Marquee/Marquee';
import { CiCell } from './CiCell';
import styles from './CommandTable.module.css';
import { CommitIcon, LayersIcon, PrIcon } from './Icons';
import { MergeCell } from './MergeCell';
import { MergeDialog } from './MergeDialog';
import { Pill } from './Pill';
import { RelativeTime } from './RelativeTime';
import { ReviewCell } from './ReviewCell';
import { TicketCell } from './TicketCell';
import { useNow } from './useNow';

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

/** A merge the server accepted, whose launch animation the list lets finish before the card goes. */
export interface Launch {
  /** The card as it was when the merge was accepted: the snapshot may have dropped it already. */
  pr: SnapshotPr;
}

interface CardProps {
  id: string;
  /** Whether the "Hide" box is checked. */
  hidden: boolean;
  /** Hidden cards shown among the others in the "All" mode. */
  dim: boolean;
  motion: Motion;
  onHide: (id: string) => void;
  /** Set while the launch of this card's merge plays. */
  launch?: Launch;
  /** Called when the server accepted a merge made from this card. */
  onMerged?: (pr: SnapshotPr, result: MergeResult) => void;
}

const MOTION_CLASS = { out: styles.ghostOut, in: styles.ghostIn, dim: styles.ghostDim } as const;

function CardView({ id, hidden, dim, motion, onHide, launch, onMerged }: CardProps) {
  const live = useSnapshotPr(id);
  const [dialog, setDialog] = useState(false);
  const pr = live ?? launch?.pr;
  if (!pr) return null;
  const url = githubPrUrl(pr.repo.nameWithOwner, pr.number);
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
          <MergeCell pr={pr} launched={launch !== undefined} onMerge={() => setDialog(true)} />
        </div>
      </div>
      {dialog && (
        <MergeDialog
          pr={pr}
          onClose={() => setDialog(false)}
          onMerged={(result) => {
            setDialog(false);
            onMerged?.(pr, result);
          }}
        />
      )}
    </article>
  );
}

export const Card = memo(CardView);
