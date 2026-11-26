import { effectiveDir, SORT_OPTIONS, type SortKey } from '@/lib/sort';
import type { HideMode } from '@/lib/view';
import { Hint } from './Hint';
import { RollSwitch, type RollOption } from './RollSwitch';
import styles from './Toolbar.module.css';

const HIDE_OPTIONS: RollOption<HideMode>[] = [
  { id: 'visible', label: 'Visible' },
  { id: 'all', label: 'All' },
  { id: 'hidden', label: 'Hidden' },
];

export interface ToolbarProps {
  sort: SortKey;
  reversed: boolean;
  onSort: (key: SortKey) => void;
  onReverse: () => void;
  mode: HideMode;
  onMode: (mode: HideMode) => void;
  /** Pull requests each hide mode would show, for the badge. */
  counts: Record<HideMode, number>;
}

export function Toolbar({ sort, reversed, onSort, onReverse, mode, onMode, counts }: ToolbarProps) {
  const dir = effectiveDir(sort, reversed);
  return (
    <div className={styles.toolbar}>
      <div className={styles.group}>
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
          data-dir={dir}
          onClick={onReverse}
        >
          <svg
            className={dir === 'desc' ? styles.arrow : `${styles.arrow} ${styles.arrowUp}`}
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            focusable="false"
          >
            <path d="M12 5v14M6 13l6 6 6-6" />
          </svg>
        </button>
      </div>
      <span className={styles.divider} aria-hidden="true" />
      <div className={styles.group}>
        <Hint label="Hidden PRs">
          <b>Hidden PRs</b>
          <span>
            <b>Visible</b> hides the pull requests you hid.
          </span>
          <span>
            <b>All</b> shows every pull request, the hidden ones dimmed.
          </span>
          <span>
            <b>Hidden</b> shows only the ones you hid.
          </span>
          <span className={styles.hintNote}>Activate the button to switch to the next state.</span>
        </Hint>
        <RollSwitch
          name="Hidden PRs"
          options={HIDE_OPTIONS}
          value={mode}
          onChange={onMode}
          count={counts[mode]}
        />
      </div>
    </div>
  );
}
