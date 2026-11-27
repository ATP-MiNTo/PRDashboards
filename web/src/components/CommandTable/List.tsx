import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { MergeResult, Snapshot, SnapshotPr } from '@/api/client';
import { useSnapshot } from '@/api/queries';
import { mergeSummary } from '@/lib/merge';
import { groupsOf, type Group } from '@/lib/snapshot';
import { sortGroups } from '@/lib/sort';
import { reducedMotion } from '@/lib/storage';
import { applyHidden, locate, paginate, pinLaunched, shownIn, type Launched } from '@/lib/view';
import { Pager } from '../Toolbar/Pager';
import type { ListPrefs } from '../Toolbar/useListPrefs';
import { useToast } from '../Toast/Toast';
import { Card, type Launch, type Motion } from './Card';
import styles from './CommandTable.module.css';
import { StackBanner } from './StackBanner';

/** How long a card fades out before it leaves the list; matches the ghost-out animation. */
export const GHOST_MS = 480;
/** How long the one-shot dim and appear animations of a card last. */
export const PULSE_MS = 500;
/** How long a merged card plays its launch before it leaves the list and the toast says what happened. */
export const LAUNCH_MS = 3000;
/** The same under reduced motion: the card says "Merged" for a moment and goes. */
export const LAUNCH_REDUCED_MS = 1200;

const NO_GROUPS: Group[] = [];

interface Playing extends Launch {
  where: Launched;
}

/** The list's own message, in the glass surface of the design. */
export function Notice({
  title,
  children,
  error,
}: {
  title?: string;
  children: string;
  error?: boolean;
}) {
  return (
    <div className={styles.empty} role={error ? 'alert' : undefined}>
      {title && <p className={styles.emptyTitle}>{title}</p>}
      <p className={error ? `${styles.emptyBody} ${styles.emptyError}` : styles.emptyBody}>
        {children}
      </p>
    </div>
  );
}

export function List({ prefs }: { prefs: ListPrefs }) {
  const { filter, terms, sort, reversed, mode, show, hidden, toggleHidden, pageSize, page } = prefs;
  const { off, notes, setNote } = prefs;
  // Merged cards stay in the list while their launch plays, even after the snapshot dropped them.
  const [playing, setPlaying] = useState<ReadonlyMap<string, Playing>>(new Map());
  const wheres = useMemo(() => [...playing.values()].map((p) => p.where), [playing]);
  const select = useCallback(
    (s: Snapshot) => {
      const byId = new Map(s.prs.map((p) => [p.id, p]));
      const groups = sortGroups(groupsOf(s, filter, show, terms, off), byId, sort, reversed);
      return pinLaunched(applyHidden(groups, hidden, mode), wheres, new Set(byId.keys()));
    },
    [filter, terms, sort, reversed, show, off, hidden, mode, wheres],
  );
  const groups = useSnapshot<Group[]>(select).data ?? NO_GROUPS;
  const toast = useToast();
  const pages = paginate(groups, pageSize);
  const current = Math.min(page, Math.max(0, pages.length - 1));

  // A card about to leave the list fades out first and the toggle is applied when it ends. One that
  // stays plays a short dim or appear animation instead.
  const [motion, setMotion] = useState<ReadonlyMap<string, Exclude<Motion, null>>>(new Map());
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  useEffect(() => {
    const all = timers.current;
    return () => all.forEach(clearTimeout);
  }, []);

  const play = useCallback((id: string, m: Exclude<Motion, null> | null) => {
    setMotion((cur) => {
      const next = new Map(cur);
      if (m) next.set(id, m);
      else next.delete(id);
      return next;
    });
  }, []);

  const hide = useCallback(
    (id: string) => {
      const isHidden = hidden.has(id);
      if (reducedMotion() || motion.has(id)) {
        toggleHidden(id);
        return;
      }
      const after = shownIn(mode, !isHidden);
      if (after) {
        // Stays in the list (All mode, or shown again): dim or appear once.
        toggleHidden(id);
        play(id, isHidden ? 'in' : 'dim');
        const t = setTimeout(() => {
          timers.current.delete(t);
          play(id, null);
        }, PULSE_MS);
        timers.current.add(t);
        return;
      }
      play(id, 'out');
      const t = setTimeout(() => {
        timers.current.delete(t);
        toggleHidden(id);
        play(id, null);
      }, GHOST_MS);
      timers.current.add(t);
    },
    [hidden, motion, mode, play, toggleHidden],
  );

  // The server accepted a merge: the card plays the launch, then leaves and the toast sums it up.
  const merged = useCallback(
    (pr: SnapshotPr, result: MergeResult) => {
      const summary = mergeSummary(pr.number, result.steps);
      if (!summary.merged) {
        toast.show(summary.text, summary.failed ? 'error' : 'info');
        return;
      }
      const where = locate(groups, pr.id);
      const done = () => {
        setPlaying((cur) => {
          const next = new Map(cur);
          next.delete(pr.id);
          return next;
        });
        toast.show(summary.text, summary.failed ? 'error' : 'success');
      };
      if (!where) {
        done();
        return;
      }
      setPlaying((cur) => new Map(cur).set(pr.id, { pr, where: { id: pr.id, ...where } }));
      const t = setTimeout(
        () => {
          timers.current.delete(t);
          done();
        },
        reducedMotion() ? LAUNCH_REDUCED_MS : LAUNCH_MS,
      );
      timers.current.add(t);
    },
    [groups, toast],
  );

  const card = (id: string) => {
    const m = motion.get(id) ?? null;
    return (
      <Card
        key={id}
        id={id}
        hidden={hidden.has(id) !== (m === 'out')}
        dim={mode === 'all' && hidden.has(id) && m !== 'out'}
        motion={m}
        onHide={hide}
        note={notes[id]}
        onNote={setNote}
        launch={playing.get(id)}
        onMerged={merged}
      />
    );
  };

  return (
    <>
      {groups.length === 0 ? (
        <Notice title="Nothing here">
          No open pull requests match these switches and filters.
        </Notice>
      ) : (
        <div className={styles.list}>
          {(pages[current] ?? []).map((g) =>
            g.stacked ? (
              <section key={g.key} className={styles.stackGroup} aria-label="Stack">
                <StackBanner ids={g.ids} />
                <div className={styles.stackList}>{g.ids.map(card)}</div>
              </section>
            ) : (
              g.ids.map(card)
            ),
          )}
        </div>
      )}
      <Pager
        pages={pages}
        page={current}
        pageSize={pageSize}
        onPage={prefs.setPage}
        onPageSize={prefs.setPageSize}
      />
    </>
  );
}
