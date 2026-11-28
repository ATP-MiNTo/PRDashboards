import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import type { Check, SnapshotPr } from '@/api/client';
import { githubChecksUrl } from '@/lib/snapshot';
import { usePopover } from '../Dropdown/usePopover';
import styles from './Controls.module.css';
import { SpinnerIcon } from './Icons';

const ROLLUP: Record<SnapshotPr['ci']['state'], { tone: string; label: string }> = {
  passing: { tone: styles.ok ?? '', label: 'Passing' },
  failing: { tone: styles.err ?? '', label: 'Fails' },
  running: { tone: styles.warn ?? '', label: 'Running' },
  none: { tone: styles.ghost ?? '', label: 'No CI' },
};

const STATE: Record<Check['state'], string> = {
  passing: 'passed',
  failing: 'failed',
  running: 'running',
  pending: 'queued',
  skipped: 'skipped',
  neutral: 'neutral',
  cancelled: 'cancelled',
};

const DOT: Partial<Record<Check['state'], string>> = {
  passing: styles.dot_passing,
  failing: styles.dot_failing,
  running: styles.dot_running,
};

/**
 * The CI cell: the rollup as an outlined badge ("Passing", "Fails", "Running"), and an "i" segment
 * that opens a panel with the checks and a link to all of them on GitHub. The panel is fixed to the
 * viewport (the card is glass, which would clip it), closes with Escape, a click outside or Tab,
 * and gives the focus back to the "i" button.
 */
export function CiCell({ pr }: { pr: SnapshotPr }) {
  const rollup = ROLLUP[pr.ci.state];
  const info = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const id = useId();
  const { pop, place } = usePopover(open, info, setOpen, {
    side: 'auto',
    align: 'end',
    gap: 6,
    matchWidth: false,
  });
  const title = `Checks for ${pr.repo.nameWithOwner} #${pr.number}`;
  const url = githubChecksUrl(pr.repo.nameWithOwner, pr.number);
  const more = pr.ci.total - pr.ci.checks.length;

  const close = (back = true) => {
    setOpen(false);
    if (back) info.current?.focus();
  };

  useEffect(() => {
    if (!open) return;
    pop.current?.focus();
    const away = (e: Event) => {
      const at = e.target;
      if (at instanceof Node && !info.current?.contains(at) && !pop.current?.contains(at)) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open, pop]);

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if (e.key === 'Tab') {
      close();
    }
  };

  return (
    <div className={styles.badgeRow}>
      <div className={`${styles.badge} ${styles.ci} ${rollup.tone}`}>
        <span className={styles.ciLabel}>
          {pr.ci.state === 'running' && <SpinnerIcon className={styles.spinner} />}
          {rollup.label}
        </span>
        <span className={styles.ciSep} aria-hidden="true" />
        <button
          ref={info}
          type="button"
          className={styles.ciInfo}
          aria-label={`Checks for #${pr.number}`}
          aria-haspopup="dialog"
          aria-expanded={open}
          aria-controls={open ? id : undefined}
          onClick={() => setOpen((o) => !o)}
        >
          <span className={styles.ciGlyph} aria-hidden="true">
            i
          </span>
        </button>
      </div>
      {open &&
        createPortal(
          <div
            ref={place}
            id={id}
            className={styles.pop}
            role="dialog"
            aria-label={title}
            tabIndex={-1}
            onKeyDown={onKeyDown}
          >
            <div className={styles.popBody}>
              <p className={styles.popTitle}>{title}</p>
              {pr.ci.checks.length > 0 ? (
                <ul className={styles.popRows} aria-label="Checks">
                  {pr.ci.checks.map((c) => (
                    <li key={`${c.name}|${c.app ?? ''}|${c.url ?? ''}`} className={styles.popRow}>
                      <span className={`${styles.dot} ${DOT[c.state] ?? ''}`} aria-hidden="true" />
                      <span className={styles.popName}>{c.name}</span>
                      <span className={styles.popState}>{STATE[c.state]}</span>
                    </li>
                  ))}
                </ul>
              ) : (
                <p className={styles.popMore}>
                  {pr.ci.total > 0
                    ? `${pr.ci.total} checks, their names are not available`
                    : 'No checks reported'}
                </p>
              )}
              {pr.ci.checks.length > 0 && more > 0 && (
                <p className={styles.popMore}>+{more} more</p>
              )}
              {url && (
                <a className={styles.popOpen} href={url} target="_blank" rel="noopener noreferrer">
                  Open all checks on GitHub ↗
                </a>
              )}
            </div>
          </div>,
          document.body,
        )}
    </div>
  );
}
