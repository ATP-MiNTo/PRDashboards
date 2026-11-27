'use client';

import { useEffect, useState, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { FOCUSABLE } from '@/lib/focus';
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
  /** The scrolling content. It stays mounted once the sidebar has opened, so drafts survive a collapse. */
  children: ReactNode;
  /** Pinned under the scrolling content, such as the accounts. */
  footer?: ReactNode;
}

const GEAR = (
  <>
    <circle cx="12" cy="12" r="3" />
    <path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z" />
  </>
);

/**
 * The left sidebar. It slides in and out over 0.3s, as a column beside the page on wide screens
 * and as a modal overlay with a scrim on narrow ones. Closed, it is inert and invisible.
 */
export function Sidebar({
  id,
  open,
  overlay,
  onClose,
  asideRef,
  returnFocus,
  children,
  footer,
}: SidebarProps) {
  const [seen, setSeen] = useState(open);
  if (open && !seen) setSeen(true);

  // The slide is on only once the page has settled, so a sidebar that opens at load does not slide.
  const [animate, setAnimate] = useState(false);
  useEffect(() => {
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => setAnimate(true));
    });
    return () => cancelAnimationFrame(frame);
  }, []);

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
    <div
      className={overlay ? styles.layer : styles.column}
      data-open={open}
      data-animate={animate}
      aria-hidden={open ? undefined : true}
      inert={!open}
      onKeyDown={onKeyDown}
    >
      {overlay && <div className={styles.scrim} data-testid="sidebar-scrim" onClick={onClose} />}
      <aside
        ref={asideRef}
        id={id}
        className={styles.sidebar}
        aria-label="Settings"
        {...(overlay ? { role: 'dialog', 'aria-modal': true, tabIndex: -1 } : {})}
      >
        <button
          type="button"
          className={styles.head}
          aria-label="Collapse sidebar"
          aria-expanded={open}
          aria-controls={id}
          title="Collapse sidebar"
          onClick={onClose}
        >
          <span className={styles.gear} aria-hidden="true">
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              focusable="false"
            >
              {GEAR}
            </svg>
          </span>
          <span className={`display ${styles.headTitle}`} aria-hidden="true">
            Settings
          </span>
          <svg
            className={styles.headChevron}
            width="16"
            height="16"
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
        <div className={styles.sidebarBody}>{seen && children}</div>
        {footer}
      </aside>
    </div>
  );
}
