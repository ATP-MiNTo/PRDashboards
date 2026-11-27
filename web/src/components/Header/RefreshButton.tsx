'use client';

import { useEffect, useId, useRef, useState, type RefObject } from 'react';
import type { Snapshot, SyncStatus } from '@/api/client';
import { useSyncNow } from '@/api/queries';
import { refreshToast } from '@/lib/refresh';
import { reducedMotion } from '@/lib/storage';
import { useToast } from '../Toast/Toast';
import styles from './Header.module.css';
import { Icon } from './Icon';
import { SyncDetails } from './SyncDetails';

/** One turn of the spinning icon, in milliseconds. */
export const SPIN_MS = 800;
/** A refresh that never shows a newer snapshot stops waiting for its toast after this long. */
const TOAST_WAIT_MS = 60_000;

/**
 * Turns the icon while `spinning`, and when the spinning stops finishes the turn it is in with a
 * slowing-down settle, so the icon never jumps back. Does nothing under reduced motion.
 */
export function useSpin(ref: RefObject<HTMLElement | null>, spinning: boolean): void {
  useEffect(() => {
    const el = ref.current;
    if (!spinning || !el || typeof el.animate !== 'function' || reducedMotion()) return;
    const turn = el.animate([{ transform: 'rotate(0deg)' }, { transform: 'rotate(360deg)' }], {
      duration: SPIN_MS,
      iterations: Infinity,
    });
    return () => {
      const angle = ((Number(turn.currentTime ?? 0) % SPIN_MS) / SPIN_MS) * 360;
      turn.cancel();
      const left = 360 - angle;
      if (left < 4 || left > 356) return;
      el.animate([{ transform: `rotate(${angle}deg)` }, { transform: 'rotate(360deg)' }], {
        duration: 250 + (left / 360) * 700,
        easing: 'cubic-bezier(.2,.7,.3,1)',
      });
    };
  }, [ref, spinning]);
}

/**
 * The refresh circle: starts a sync, spins while one runs, and shows the sync details in a popover
 * on hover and keyboard focus. A refresh that this button started ends with a toast.
 */
export function RefreshButton({
  snapshot,
  status,
}: {
  snapshot: Snapshot | undefined;
  status: SyncStatus | null | undefined;
}) {
  const sync = useSyncNow();
  const toast = useToast();
  const running = status?.running === true || sync.isPending;
  const icon = useRef<HTMLSpanElement>(null);
  useSpin(icon, running);

  const popId = useId();
  const [hover, setHover] = useState(false);
  const [focus, setFocus] = useState(false);
  const open = hover || focus;
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      setHover(false);
      setFocus(false);
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open]);

  // The toast waits for the refresh to end. A refresh that finds nothing new keeps the snapshot
  // and its version, so the status (its count of finished refreshes and the snapshot version it
  // saw) tells that case from a newer snapshot that is still on its way.
  const asked = useRef<{ snapshot: Snapshot; refreshes: number } | null>(null);
  const giveUp = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(giveUp.current), []);
  useEffect(() => {
    const before = asked.current;
    if (!before || running || !snapshot) return;
    const newer = snapshot.version > before.snapshot.version;
    const unchanged =
      status != null &&
      (status.refreshes ?? 0) > before.refreshes &&
      status.snapshotVersion === before.snapshot.version;
    if (!newer && !unchanged) return;
    asked.current = null;
    clearTimeout(giveUp.current);
    const t = refreshToast(before.snapshot, snapshot, Date.now());
    toast.show(t.message, t.kind);
  }, [running, snapshot, status, toast]);

  const onClick = () => {
    if (running) return;
    if (snapshot) {
      asked.current = { snapshot, refreshes: status?.refreshes ?? 0 };
      clearTimeout(giveUp.current);
      giveUp.current = setTimeout(() => (asked.current = null), TOAST_WAIT_MS);
    }
    sync.mutate();
  };

  return (
    <span
      className={styles.refreshWrap}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <button
        type="button"
        className={styles.ctlButton}
        aria-label="Refresh"
        aria-busy={running ? true : undefined}
        aria-describedby={open ? popId : undefined}
        data-running={running || undefined}
        onClick={onClick}
        onFocus={() => setFocus(true)}
        onBlur={() => setFocus(false)}
      >
        <span ref={icon} className={styles.iconBox}>
          <Icon>
            <path d="M21 12a9 9 0 1 1-3-6.7L21 8" />
            <path d="M21 3v5h-5" />
          </Icon>
        </span>
      </button>
      {open && (
        <span id={popId} role="tooltip" className={styles.pop}>
          <span className={styles.popCard}>
            <SyncDetails snapshot={snapshot} status={status} />
          </span>
        </span>
      )}
    </span>
  );
}
