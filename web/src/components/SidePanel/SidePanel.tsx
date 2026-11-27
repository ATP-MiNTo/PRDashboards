'use client';

import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react';
import styles from './SidePanel.module.css';

export const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export interface SidePanelProps {
  open: boolean;
  onClose: () => void;
  /** The heading and the accessible name of the dialog. */
  title: string;
  /** The accessible name of the close button. Defaults to "Close" and the lower-cased title. */
  closeLabel?: string;
  children: ReactNode;
}

/** A modal panel that slides in from the right: scrim, header with a close button, focus trap, Escape, and focus restore. */
export function SidePanel({ open, onClose, title, closeLabel, children }: SidePanelProps) {
  const dialog = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!open) return;
    const before = document.activeElement as HTMLElement | null;
    dialog.current?.focus();
    return () => before?.focus();
  }, [open]);

  if (!open) return null;

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== 'Tab' || !dialog.current) return;
    const items = [...dialog.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) return;
    const at = document.activeElement;
    if (e.shiftKey && (at === first || at === dialog.current)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    <div className={styles.layer} onKeyDown={onKeyDown}>
      <div className={styles.scrim} data-testid="side-panel-scrim" onClick={onClose} />
      <aside
        ref={dialog}
        className={styles.panel}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        tabIndex={-1}
      >
        <header className={styles.head}>
          <h2>{title}</h2>
          <button
            type="button"
            className={styles.close}
            aria-label={closeLabel ?? `Close ${title.toLowerCase()}`}
            onClick={onClose}
          >
            Close
          </button>
        </header>
        <div className={styles.content}>{children}</div>
      </aside>
    </div>
  );
}
