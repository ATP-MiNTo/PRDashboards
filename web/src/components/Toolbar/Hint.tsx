import { useEffect, useId, useState, type ReactNode } from 'react';
import styles from './Toolbar.module.css';

export interface HintProps {
  /** The visible label that carries the hint. */
  label: string;
  children: ReactNode;
}

/**
 * A label with a popover that opens on hover and on keyboard focus, and closes on Escape, blur or
 * when the pointer leaves.
 */
export function Hint({ label, children }: HintProps) {
  const id = useId();
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

  return (
    <span
      className={styles.hintWrap}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
    >
      <span
        className={styles.hintLabel}
        tabIndex={0}
        aria-describedby={open ? id : undefined}
        onFocus={() => setFocus(true)}
        onBlur={() => setFocus(false)}
      >
        {label}
      </span>
      {open && (
        <span role="tooltip" id={id} className={styles.hintPop}>
          {children}
        </span>
      )}
    </span>
  );
}
