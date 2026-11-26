import { useEffect, useRef, useState } from 'react';
import { reducedMotion } from '@/lib/storage';
import styles from './Toolbar.module.css';

export interface RollOption<T extends string> {
  id: T;
  label: string;
}

export interface RollSwitchProps<T extends string> {
  /** What the switch controls, for the accessible name and the announcement. */
  name: string;
  options: readonly RollOption<T>[];
  value: T;
  onChange: (next: T) => void;
  /** The number of items the current state shows, as a badge. */
  count: number;
}

const ROLL_MS = 300;

/** A button that cycles through its options; the label rolls upward as it changes. */
export function RollSwitch<T extends string>({
  name,
  options,
  value,
  onChange,
  count,
}: RollSwitchProps<T>) {
  const current = options.find((o) => o.id === value) ?? options[0];
  const [from, setFrom] = useState<string | null>(null);
  const [announce, setAnnounce] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  if (!current) return null;

  const click = () => {
    const i = options.indexOf(current);
    const next = options[(i + 1) % options.length];
    if (!next) return;
    if (!reducedMotion()) {
      setFrom(current.label);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setFrom(null), ROLL_MS);
    }
    setAnnounce(`${name}: ${next.label}`);
    onChange(next.id);
  };

  return (
    <>
      <button
        type="button"
        className={styles.roll}
        aria-label={`${name}, now ${current.label}, ${count}. Activate to switch.`}
        onClick={click}
      >
        <span className={styles.rollWin} aria-hidden="true">
          <span key={value} className={from === null ? styles.rollStill : styles.rollIn}>
            {current.label}
          </span>
          {from !== null && <span className={styles.rollOut}>{from}</span>}
        </span>
        <span className={styles.badge} aria-hidden="true">
          {count}
        </span>
      </button>
      <span role="status" className={styles.sr}>
        {announce}
      </span>
    </>
  );
}
