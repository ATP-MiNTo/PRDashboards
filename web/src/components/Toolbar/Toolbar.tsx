import { useState } from 'react';
import { SORT_OPTIONS, type SortKey } from '@/lib/sort';
import type { ShowMode } from '@/lib/show';
import type { HideMode } from '@/lib/view';
import { Hint } from './Hint';
import { RollSwitch, type RollOption } from './RollSwitch';
import styles from './Toolbar.module.css';

const HIDE_OPTIONS: RollOption<HideMode>[] = [
  { id: 'visible', label: 'Visible' },
  { id: 'all', label: 'All' },
  { id: 'hidden', label: 'Hidden' },
];

const SHOW_OPTIONS: RollOption<ShowMode>[] = [
  { id: 'all', label: 'All' },
  { id: 'mine', label: 'Mine', tone: 'green' },
  { id: 'review', label: 'To review', tone: 'red' },
];

const LEGEND: { id: string; label: string; hint?: string }[] = [
  { id: 'mine', label: 'Mine', hint: 'Assigned to you or opened by you' },
  { id: 'reviewer', label: 'Review requested' },
  { id: 'stack', label: 'Stack' },
];

/** Said at the end of both hints, as in the design. */
const SWITCH_NOTE = 'Click the button to switch to the next state.';

export interface ToolbarProps {
  sort: SortKey;
  reversed: boolean;
  onSort: (key: SortKey) => void;
  onReverse: () => void;
  show: ShowMode;
  onShow: (show: ShowMode) => void;
  /** Pull requests each Show state lists, across the whole snapshot, for the badge. */
  showCounts: Record<ShowMode, number>;
  mode: HideMode;
  onMode: (mode: HideMode) => void;
  /** Pull requests each hide mode would show, for the badge. */
  counts: Record<HideMode, number>;
}

export function Toolbar({
  sort,
  reversed,
  onSort,
  onReverse,
  show,
  onShow,
  showCounts,
  mode,
  onMode,
  counts,
}: ToolbarProps) {
  // The arrow plays its swing only after a click, not when the page first draws it.
  const [swung, setSwung] = useState(false);
  const arrow = [styles.arrow, reversed ? styles.arrowDown : styles.arrowUp];
  if (swung) arrow.push(styles.arrowSwing);
  return (
    <div className={styles.toolrow}>
      <div className={styles.sortbar}>
        <label htmlFor="sort-select" className={styles.eyebrow}>
          Sort
        </label>
        <select
          id="sort-select"
          className={styles.select}
          value={sort}
          onChange={(e) => onSort(e.target.value as SortKey)}
        >
          {SORT_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          className={styles.square}
          aria-label="Reverse sort order"
          aria-pressed={reversed}
          onClick={() => {
            setSwung(true);
            onReverse();
          }}
        >
          <svg
            className={styles.sortIcon}
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
            <g className={arrow.join(' ')}>
              <path d="M12 19V5M6 11l6-6 6 6" />
            </g>
          </svg>
        </button>
        <span className={styles.divider} aria-hidden="true" />
        <div className={styles.switchGroup}>
          <Hint label="Show">
            <span className={styles.hintTitle}>Show</span>
            <span>
              <b>All</b> Everything.
            </span>
            <span>
              <b>Mine</b> Assigned to you or opened by you.
            </span>
            <span>
              <b>To review</b> Waiting on your review.
            </span>
            <span className={styles.hintNote}>{SWITCH_NOTE}</span>
          </Hint>
          <div className={styles.switchBox}>
            <RollSwitch
              name="Show"
              options={SHOW_OPTIONS}
              value={show}
              onChange={onShow}
              count={showCounts[show]}
            />
          </div>
        </div>
        <div className={styles.switchGroup}>
          <Hint label="Hidden PRs">
            <span className={styles.hintTitle}>Hidden PRs</span>
            <span>
              <b>Visible</b> Hidden PRs left out.
            </span>
            <span>
              <b>All</b> Hidden PRs dimmed.
            </span>
            <span>
              <b>Hidden</b> Only PRs you hid.
            </span>
            <span className={styles.hintNote}>{SWITCH_NOTE}</span>
          </Hint>
          <div className={styles.switchBox}>
            <RollSwitch
              name="Hidden PRs"
              options={HIDE_OPTIONS}
              value={mode}
              onChange={onMode}
              count={counts[mode]}
            />
          </div>
        </div>
      </div>
      <ul className={styles.legend} aria-label="Legend">
        {LEGEND.map((l) => (
          <li key={l.id} className={styles.legendItem} title={l.hint}>
            <span className={`${styles.legendDot} ${styles[`dot_${l.id}`]}`} aria-hidden="true" />
            {l.label}
          </li>
        ))}
      </ul>
    </div>
  );
}
