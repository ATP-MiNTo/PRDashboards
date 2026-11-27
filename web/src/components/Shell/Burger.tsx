import type { RefObject } from 'react';
import { BrandIcon } from './BrandIcon';
import styles from './Shell.module.css';

export interface BurgerProps {
  open: boolean;
  /** True once the page has scrolled: the pill gets its frosted background and shows the app name. */
  stuck: boolean;
  onToggle: () => void;
  buttonRef: RefObject<HTMLButtonElement | null>;
  controls: string;
}

/** The sidebar toggle. It stays at the top left of the page; the wrapper takes no height. */
export function Burger({ open, stuck, onToggle, buttonRef, controls }: BurgerProps) {
  return (
    <div className={styles.burgerWrap}>
      <div className={stuck ? `${styles.pill} ${styles.stuck}` : styles.pill} data-stuck={stuck}>
        <button
          ref={buttonRef}
          type="button"
          className={styles.burger}
          aria-label="Settings sidebar"
          aria-expanded={open}
          aria-controls={controls}
          onClick={onToggle}
        >
          <svg
            width="20"
            height="20"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            aria-hidden="true"
            focusable="false"
          >
            <path d="M4 6h16M4 12h16M4 18h16" />
          </svg>
        </button>
        <span className={styles.pillTitle} aria-hidden="true">
          <span>PR Dashboards</span>
          <BrandIcon />
        </span>
      </div>
    </div>
  );
}
