import { useEffect, useRef, useState } from 'react';
import { reducedMotion } from '@/lib/storage';
import styles from './Toolbar.module.css';

export interface RollOption<T extends string> {
  id: T;
  label: string;
}

/**
 * An option that means "use the default" instead of a value. With it the switch has two states: the
 * default, or an explicit value.
 */
export interface DefaultChoice<T extends string> {
  /** The id of the option that means "use the default". */
  id: T;
  /** The explicit option the default resolves to now: a click from the default goes to the one after it. */
  resolvesTo?: T;
  /** The explicit option to go back to when the default is switched off. Without it, `resolvesTo`. */
  remembered?: T;
}

/** What the tooltip and the accessible name say about the two states of a switch with a default. */
export const ROLL_DEFAULT_HINT =
  'Click: next choice. Right-click, Shift+click or Shift+Enter: use default or choose';

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
  /**
   * Makes one option the "use the default" state. A click then turns through the other options only,
   * and a right-click, the Menu key, Shift+click or Shift+Enter (a keyboard "click" carries the Shift
   * state) switches the default on or off. Without it every click turns through all the options.
   */
  defaultChoice?: DefaultChoice<T>;
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
  defaultChoice,
}: RollSwitchProps<T>) {
  const current = options.find((o) => o.id === value) ?? options[0];
  const [from, setFrom] = useState<string | null>(null);
  const [announce, setAnnounce] = useState('');
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  if (!current) return null;

  const go = (next: RollOption<T> | undefined) => {
    if (!next) return;
    if (!reducedMotion()) {
      setFrom(current.label);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setFrom(null), ROLL_MS);
    }
    setAnnounce(`${name}: ${next.label}`);
    onChange(next.id);
  };

  // A switch with a default has nothing to roll to when the turn has one option only.
  const goIfOther = (next: RollOption<T> | undefined) => {
    if (next && next.id !== current.id) go(next);
  };

  const dc = defaultChoice;
  const explicit = options.filter((o) => o.id !== dc?.id);
  const inDefault = dc !== undefined && current.id === dc.id;

  // Without a default every option is in the turn. With one, the turn has the explicit options
  // only, and from the default it starts after the option the default resolves to.
  const click = () => {
    if (!dc) return go(options[(options.indexOf(current) + 1) % options.length]);
    const base = inDefault ? dc.resolvesTo : current.id;
    const i = explicit.findIndex((o) => o.id === base);
    goIfOther(explicit[(i + 1) % explicit.length]);
  };

  // The default is switched on, or off again to the value that was remembered.
  const toggleDefault = () => {
    if (!dc) return;
    if (!inDefault) return goIfOther(options.find((o) => o.id === dc.id));
    goIfOther(
      explicit.find((o) => o.id === dc.remembered) ??
        explicit.find((o) => o.id === dc.resolvesTo) ??
        explicit[0],
    );
  };

  return (
    <>
      <button
        type="button"
        className={variant === 'field' ? `${styles.roll} ${styles.rollField}` : styles.roll}
        aria-label={`${name}, now ${current.label}${count === undefined ? '' : `, ${count}`}. ${dc ? `${ROLL_DEFAULT_HINT}.` : 'Activate to switch.'}`}
        title={dc ? ROLL_DEFAULT_HINT : undefined}
        onClick={(e) => (dc && e.shiftKey ? toggleDefault() : click())}
        onContextMenu={
          dc
            ? (e) => {
                e.preventDefault();
                toggleDefault();
              }
            : undefined
        }
      >
        <span className={styles.rollWin} aria-hidden="true">
          {/* While it rolls, the old label sits above the new one and both slide up together. */}
          <span key={`${value}${from ?? ''}`} className={from === null ? undefined : styles.rollGo}>
            {from !== null && <span className={styles.rollLine}>{from}</span>}
            <span className={styles.rollLine}>{current.label}</span>
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
