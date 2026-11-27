'use client';

import { useId, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { usePopover } from '../Dropdown/usePopover';
import styles from './InfoTip.module.css';

/**
 * The design's "i" button: a small round ghost button that shows a dark note under it while the
 * pointer is on it or it has keyboard focus. The note is also the button's accessible description,
 * so nothing depends on hovering.
 */
export function InfoTip({ tip, label = 'About this section' }: { tip: string; label?: string }) {
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const { place } = usePopover(open, button, setOpen, {
    side: 'below',
    gap: 6,
    matchWidth: false,
    shift: -8,
  });
  return (
    <>
      <button
        ref={button}
        type="button"
        className={styles.info}
        aria-label={label}
        aria-describedby={id}
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        onFocus={() => setOpen(true)}
        onBlur={() => setOpen(false)}
        onKeyDown={(e) => {
          if (e.key === 'Escape' && open) {
            e.stopPropagation();
            setOpen(false);
          }
        }}
      >
        <span aria-hidden="true">i</span>
      </button>
      <span id={id} className={styles.sr}>
        {tip}
      </span>
      {open &&
        createPortal(
          <div ref={place} className={styles.bubble} aria-hidden="true">
            {tip}
          </div>,
          document.body,
        )}
    </>
  );
}
