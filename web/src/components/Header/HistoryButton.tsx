'use client';

import { useState } from 'react';
import styles from './Header.module.css';
import { Icon } from './Icon';

/**
 * The history circle: toggles the history view. After the first toggle the clock hand winds three
 * turns one way when the view opens and the other way when it closes.
 */
export function HistoryButton({ active, onToggle }: { active: boolean; onToggle: () => void }) {
  const [used, setUsed] = useState(false);
  const wind = used ? (active ? styles.unwind : styles.wind) : undefined;
  return (
    <button
      type="button"
      className={active ? `${styles.ctlButton} ${styles.ctlActive}` : styles.ctlButton}
      aria-label="History"
      aria-pressed={active}
      title="History"
      onClick={() => {
        setUsed(true);
        onToggle();
      }}
    >
      <Icon>
        <circle cx="12" cy="12" r="9" />
        <g className={wind ? `${styles.hand} ${wind}` : styles.hand}>
          <path d="M12 6.500V12" />
          <path d="m12 12 3.200 2" />
        </g>
      </Icon>
    </button>
  );
}
