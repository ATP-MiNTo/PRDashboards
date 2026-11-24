import { statsOf, type Filter, type Stats } from '@/lib/snapshot';
import type { Snapshot } from '@/api/client';
import styles from './CommandTable.module.css';
import { useNow } from './useNow';

const TILES: { key: keyof Stats; label: string }[] = [
  { key: 'open', label: 'Open' },
  { key: 'ready', label: 'Ready' },
  { key: 'blocked', label: 'Blocked' },
  { key: 'draft', label: 'Draft' },
  { key: 'stale', label: 'Stale' },
];

const CHIPS: { value: Filter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'ready', label: 'Ready' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'draft', label: 'Draft' },
  { value: 'stale', label: 'Stale' },
];

export function StatStrip({ snapshot }: { snapshot: Snapshot }) {
  const now = useNow(60_000);
  const stats = statsOf(snapshot, now);
  return (
    <dl className={styles.stats}>
      {TILES.map((t) => (
        <div key={t.key} className={styles.stat}>
          <dt>{t.label}</dt>
          <dd>{stats[t.key]}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Filters({ value, onChange }: { value: Filter; onChange: (f: Filter) => void }) {
  return (
    <div className={styles.chips} role="group" aria-label="Filter">
      {CHIPS.map((c) => (
        <button
          key={c.value}
          type="button"
          aria-pressed={value === c.value}
          onClick={() => onChange(c.value)}
        >
          {c.label}
        </button>
      ))}
    </div>
  );
}
