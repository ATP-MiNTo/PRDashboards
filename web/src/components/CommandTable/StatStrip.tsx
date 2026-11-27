import type { Snapshot } from '@/api/client';
import { useSnapshot } from '@/api/queries';
import type { Filter } from '@/lib/snapshot';
import { listStats, type ListStats, type ListView } from '@/lib/view';
import styles from './CommandTable.module.css';

const TILES: { key: keyof ListStats; label: string; filter: Filter; hl?: boolean }[] = [
  { key: 'showing', label: 'Showing', filter: 'all' },
  { key: 'ready', label: 'Ready to merge', filter: 'ready', hl: true },
  { key: 'running', label: 'Running CI', filter: 'running' },
  { key: 'stacks', label: 'Stacks', filter: 'stacks' },
];

/**
 * One strip of tiles: how many pull requests the list shows, how many are ready to merge, how many
 * run CI and how many stacks there are, all for what the Show, hide and search switches leave. A tile
 * is a button that filters the list to its pull requests ("Showing" lifts the filter).
 */
export function StatStrip({ view, onFilter }: { view: ListView; onFilter: (f: Filter) => void }) {
  const stats = useSnapshot((s: Snapshot) => listStats(s, view)).data;
  if (!stats) return null;
  return (
    <div className={styles.stats} role="group" aria-label="Summary">
      {TILES.map((t) => {
        const on = view.filter === t.filter;
        return (
          <button
            key={t.key}
            type="button"
            className={on && t.filter !== 'all' ? `${styles.stat} ${styles.statOn}` : styles.stat}
            aria-pressed={on}
            onClick={() => onFilter(t.filter)}
          >
            <span className={styles.statTitle}>{t.label}</span>
            <span className={t.hl ? `${styles.statValue} ${styles.statHl}` : styles.statValue}>
              {stats[t.key]}
            </span>
          </button>
        );
      })}
    </div>
  );
}
