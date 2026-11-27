'use client';

import { useEffect, useRef, type KeyboardEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { FOCUSABLE } from '@/lib/focus';
import styles from './Modal.module.css';

export interface ModalProps {
  /** The id of the element that names the dialog (its heading). */
  labelledBy: string;
  onClose: () => void;
  /** While false the scrim and Escape do nothing, for a request that must finish first. */
  dismissible?: boolean;
  /** The wide box of the rules dialog: at most 48rem instead of 32rem. */
  wide?: boolean;
  children: ReactNode;
}

/**
 * A centred dialog over a scrim: rendered at the end of the body (a glass card is a containing
 * block for fixed children), focus moves into it, Tab stays inside, Escape and a click on the scrim
 * close it, and focus goes back to where it was.
 */
export function Modal({
  labelledBy,
  onClose,
  dismissible = true,
  wide = false,
  children,
}: ModalProps) {
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const el = box.current;
    if (el && !el.contains(document.activeElement)) el.focus();
    return () => before?.focus?.();
  }, []);

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      if (dismissible) onClose();
      return;
    }
    if (e.key !== 'Tab' || !box.current) return;
    const items = [...box.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) {
      e.preventDefault();
      return;
    }
    const at = document.activeElement;
    if (e.shiftKey && (at === first || at === box.current)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return createPortal(
    <div className={styles.layer} onKeyDown={onKeyDown}>
      <div
        className={styles.scrim}
        data-testid="modal-scrim"
        onClick={() => dismissible && onClose()}
      />
      <div
        ref={box}
        className={wide ? `${styles.box} ${styles.wide}` : styles.box}
        role="dialog"
        aria-modal="true"
        aria-labelledby={labelledBy}
        tabIndex={-1}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}
