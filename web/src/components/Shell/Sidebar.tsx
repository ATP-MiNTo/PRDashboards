'use client';

import { useEffect, useState, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { FOCUSABLE } from '../SidePanel/SidePanel';
import styles from './Shell.module.css';

export interface SidebarProps {
  id: string;
  open: boolean;
  /** Narrow screens: a modal overlay with a scrim. Wide screens: a column beside the page. */
  overlay: boolean;
  onClose: () => void;
  asideRef: RefObject<HTMLElement | null>;
  /** Where focus goes when the overlay closes. */
  returnFocus: RefObject<HTMLElement | null>;
  children: ReactNode;
}

/** The left sidebar with the Settings content. Its content stays mounted once opened, so drafts survive a collapse. */
export function Sidebar({
  id,
  open,
  overlay,
  onClose,
  asideRef,
  returnFocus,
  children,
}: SidebarProps) {
  const [seen, setSeen] = useState(open);
  if (open && !seen) setSeen(true);

  const modal = overlay && open;
  useEffect(() => {
    if (!modal) return;
    asideRef.current?.focus();
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    const back = returnFocus.current;
    return () => {
      document.removeEventListener('keydown', onKey);
      back?.focus();
    };
  }, [modal, asideRef, onClose, returnFocus]);

  const onKeyDown = (e: KeyboardEvent) => {
    if (!modal || e.key !== 'Tab' || !asideRef.current) return;
    const items = [...asideRef.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) return;
    const at = document.activeElement;
    if (e.shiftKey && (at === first || at === asideRef.current)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    <div className={overlay ? styles.layer : styles.column} hidden={!open} onKeyDown={onKeyDown}>
      {overlay && <div className={styles.scrim} data-testid="sidebar-scrim" onClick={onClose} />}
      <aside
        ref={asideRef}
        id={id}
        className={styles.sidebar}
        aria-label="Settings"
        {...(overlay ? { role: 'dialog', 'aria-modal': true, tabIndex: -1 } : {})}
      >
        <header className={styles.sidebarHead}>
          <h2>Settings</h2>
          <button
            type="button"
            className={styles.collapse}
            aria-label="Collapse sidebar"
            aria-expanded={open}
            aria-controls={id}
            onClick={onClose}
          >
            <svg
              width="18"
              height="18"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
              focusable="false"
            >
              <path d="m15 6-6 6 6 6" />
            </svg>
          </button>
        </header>
        <div className={styles.sidebarBody}>{seen && children}</div>
      </aside>
    </div>
  );
}
