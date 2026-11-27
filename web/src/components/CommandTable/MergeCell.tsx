import { useEffect, useId, useRef, useState } from 'react';
import { ApiError, type SnapshotPr } from '@/api/client';
import { useAccounts, useMarkReady } from '@/api/queries';
import { describeReviewProblem } from '@/lib/problems';
import { blockOf, readyBlockOf } from '@/lib/snapshot';
import { Tip } from '../Tip/Tip';
import { useToast } from '../Toast/Toast';
import styles from './Controls.module.css';
import { RocketIcon } from './Icons';
import { MergeIcon } from './MergeIcon';

/** How long the frost sweep after "Ready" plays. */
const COLD_MS = 1300;
/** How long a draft that was just marked ready is shown as waiting for its checks, if the snapshot is slow to follow. */
const READIED_MS = 30_000;
const COLD_TIP = 'Checks restart after leaving draft.';
const DRAFT_TIP = 'Draft. Mark ready for review to merge.';

/** The button once the server accepted the merge: a rocket crosses it, a sonic boom follows, "Merged". */
function MergedButton() {
  return (
    <span className={styles.mflyWrap}>
      <span className={styles.mfly} role="status" aria-label="Merged">
        <span className={styles.mStars} />
        <span className={styles.mOld}>
          Merge <MergeIcon />
        </span>
        <span className={styles.mNew}>Merged</span>
        <span className={styles.mRocket}>
          <RocketIcon />
        </span>
      </span>
      <span className={styles.mBoom} />
      <span className={`${styles.mBoom} ${styles.mBoom2}`} />
    </span>
  );
}

/** A button that looks and reads disabled, but can be focused so that its reason is read out. */
function Blocked({
  reason,
  className,
  label = 'Merge',
}: {
  reason: string;
  className: string | undefined;
  label?: string;
}) {
  const id = useId();
  return (
    <Tip tip={reason} side="left">
      <button
        type="button"
        className={className}
        aria-disabled="true"
        aria-describedby={id}
        onClick={(e) => e.preventDefault()}
      >
        {label}
      </button>
      <span id={id} className={styles.srOnly}>
        {reason}
      </span>
    </Tip>
  );
}

/**
 * The Merge cell of a card: the blue Merge button when the pull request is ready, the green Ready
 * button for a draft, and otherwise a disabled Merge with the reason as a tooltip on its left. A
 * repository the account can only read disables Merge, and Ready unless the viewer opened the draft.
 * `launched` shows the launch animation after the server accepted the merge.
 */
export function MergeCell({
  pr,
  launched,
  onMerge,
}: {
  pr: SnapshotPr;
  launched: boolean;
  onMerge: () => void;
}) {
  const block = blockOf(pr);
  const login = useAccounts().data?.github.login ?? undefined;
  const ready = useMarkReady();
  const toast = useToast();
  // After "Ready" the snapshot can still say draft for a moment: the card keeps showing the frosted,
  // disabled Merge until it catches up (or for 30 s).
  const [readied, setReadied] = useState(false);
  const [cold, setCold] = useState(false);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  useEffect(() => {
    const all = timers.current;
    return () => all.forEach(clearTimeout);
  }, []);
  const later = (fn: () => void, ms: number) => {
    const t = setTimeout(() => {
      timers.current.delete(t);
      fn();
    }, ms);
    timers.current.add(t);
  };
  const draftId = useId();

  const markReady = () =>
    ready.mutate(
      { id: pr.id, body: { expectedHeadSha: pr.headSha } },
      {
        onSuccess: () => {
          setReadied(true);
          setCold(true);
          later(() => setCold(false), COLD_MS);
          later(() => setReadied(false), READIED_MS);
          toast.show(`#${pr.number} is no longer a draft`, 'info');
        },
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

  if (launched) return <MergedButton />;

  if (cold) return <Blocked reason={COLD_TIP} className={styles.cold} />;
  if (pr.isDraft && readied) return <Blocked reason={COLD_TIP} className={styles.mergeOff} />;

  if (pr.isDraft) {
    // The account cannot write here and did not open the draft: GitHub would refuse, so say so.
    const readOnly = readyBlockOf(pr, login);
    if (readOnly) return <Blocked reason={readOnly} className={styles.mergeOff} label="Ready" />;
    return (
      <Tip tip={DRAFT_TIP} side="left">
        <button
          type="button"
          className={styles.readyButton}
          aria-describedby={draftId}
          aria-busy={ready.isPending}
          disabled={ready.isPending}
          onClick={markReady}
        >
          Ready
        </button>
        <span id={draftId} className={styles.srOnly}>
          {DRAFT_TIP}
        </span>
      </Tip>
    );
  }

  if (block.reason !== null) {
    return <Blocked reason={block.reason} className={styles.mergeOff} />;
  }

  return (
    <button type="button" className={styles.mergeButton} onClick={onMerge}>
      Merge
      <MergeIcon />
    </button>
  );
}
