import { useEffect, useRef, useState } from 'react';
import { reducedMotion } from '@/lib/storage';
import styles from './Toolbar.module.css';

/** The colour of the dot an option shows before its label, the same green and red as the cards. */
export type RollTone = 'green' | 'red';

export interface RollOption<T extends string> {
  id: T;
  label: string;
  /** A dot of this colour before the label. Without it there is no dot. */
  tone?: RollTone;
}

export interface RollSwitchProps<T extends string> {
  /** What the switch controls, for the accessible name and the announcement. */
  name: string;
  options: readonly RollOption<T>[];
  value: T;
  onChange: (next: T) => void;
  /** The number of items the current state shows, as a badge. Without it there is no badge. */
  count?: number;
  /** "field": the roll of a dialog, as wide as its column with the label 12px in. */
  variant?: 'toolbar' | 'field';
}

export const ROLL_MS = 300;

/** A button that cycles through its options; the label rolls upward as it changes. */
export function RollSwitch<T extends string>({
  name,
  options,
  value,
  onChange,
  count,
  variant = 'toolbar',
}: RollSwitchProps<T>) {
  const current = options.find((o) => o.id === value) ?? options[0];
  const [from, setFrom] = useState<RollOption<T> | null>(null);
  const [announce, setAnnounce] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  if (!current) return null;

  const click = () => {
    const i = options.indexOf(current);
    const next = options[(i + 1) % options.length];
    if (!next) return;
    if (!reducedMotion()) {
      setFrom(current);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setFrom(null), ROLL_MS);
    }
    setAnnounce(`${name}: ${next.label}`);
    onChange(next.id);
  };

  const line = (o: RollOption<T>) => (
    <span className={styles.rollLine}>
      {o.tone && <span className={`${styles.rollDot} ${styles[`rollDot_${o.tone}`]}`} />}
      {o.label}
    </span>
  );

  return (
    <>
      <button
        type="button"
        className={variant === 'field' ? `${styles.roll} ${styles.rollField}` : styles.roll}
        aria-label={`${name}, now ${current.label}${count === undefined ? '' : `, ${count}`}. Activate to switch.`}
        onClick={click}
      >
        <span className={styles.rollWin} aria-hidden="true">
          {/* While it rolls, the old label sits above the new one and both slide up together. */}
          <span
            key={`${value}${from?.id ?? ''}`}
            className={from === null ? undefined : styles.rollGo}
          >
            {from !== null && line(from)}
            {line(current)}
          </span>
        </span>
        {count !== undefined && (
          <span className={styles.badge} aria-hidden="true">
            {count}
          </span>
        )}
      </button>
      <span role="status" className={styles.sr}>
        {announce}
      </span>
    </>
  );
}
