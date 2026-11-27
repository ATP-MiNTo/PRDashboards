import type { Ref } from 'react';
import type { Snapshot } from '@/api/client';
import { useSnapshot } from '@/api/queries';
import type { Filter } from '@/lib/snapshot';
import { listStats, type ListStats, type ListView } from '@/lib/view';
import styles from './CommandTable.module.css';

interface Tile {
  key: keyof ListStats;
  label: string;
  /** The words of the compact summary in the header, where there is little room. */
  short: string;
  filter: Filter;
  hl?: boolean;
}

/** In this order, "Showing" first: the unfiltered list, then the statuses, then the stacks. */
export const TILES: Tile[] = [
  { key: 'showing', label: 'Showing', short: 'Showing', filter: 'all' },
  { key: 'ready', label: 'Ready to merge', short: 'Ready', filter: 'ready', hl: true },
  { key: 'running', label: 'Running CI', short: 'CI', filter: 'running' },
  { key: 'blocked', label: 'Blocked', short: 'Blocked', filter: 'blocked' },
  { key: 'draft', label: 'Draft', short: 'Draft', filter: 'draft' },
  { key: 'stale', label: 'Stale', short: 'Stale', filter: 'stale' },
  { key: 'stacks', label: 'Stacks', short: 'Stacks', filter: 'stacks' },
];

/** The numbers of the tiles for what the list shows, or undefined before the snapshot is there. */
export function useListStats(view: ListView): ListStats | undefined {
  return useSnapshot((s: Snapshot) => listStats(s, view)).data;
}

interface TilesProps {
  stats: ListStats;
  filter: Filter;
  onFilter: (f: Filter) => void;
}

/**
 * One strip of tiles: how many pull requests the list shows, how many are ready to merge, run CI,
 * are blocked, drafts or stale, and how many stacks there are, all for what the Show, hide and
 * search switches leave. A tile is a button that filters the list to its pull requests ("Showing"
 * lifts the filter). The strip wraps onto more rows when it is narrow.
 */
export function StatStrip({
  stats,
  filter,
  onFilter,
  stripRef,
}: TilesProps & { stripRef?: Ref<HTMLDivElement> }) {
  return (
    <div ref={stripRef} className={styles.stats} role="group" aria-label="Summary">
      {TILES.map((t) => {
        const on = filter === t.filter;
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

/**
 * The same tiles in a line, for the header pill once the strip has scrolled out of view. The same
 * buttons with the same filters; the name of each one says its number.
 */
export function CompactStats({ stats, filter, onFilter }: TilesProps) {
  return (
    <>
      {TILES.map((t) => {
        const on = filter === t.filter;
        return (
          <button
            key={t.key}
            type="button"
            className={on && t.filter !== 'all' ? `${styles.cs} ${styles.csOn}` : styles.cs}
            aria-pressed={on}
            aria-label={`${t.label}: ${stats[t.key]}`}
            onClick={() => onFilter(t.filter)}
          >
            <span className={styles.csLabel} aria-hidden="true">
              {t.short}
            </span>{' '}
            <b
              className={t.hl ? `${styles.csValue} ${styles.csHl}` : styles.csValue}
              aria-hidden="true"
            >
              {stats[t.key]}
            </b>
          </button>
        );
      })}
    </>
  );
}
