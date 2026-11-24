import { useCallback } from 'react';
import type { Snapshot } from '@/api/client';
import { useSnapshot } from '@/api/queries';
import { groupsOf, type Filter, type Group } from '@/lib/snapshot';
import styles from './CommandTable.module.css';
import { Pill } from './Pill';
import { Row } from './Row';

const COLUMNS = ['Repo / PR', 'Ticket', 'Last commit', 'Review', 'CI', 'Method', 'Merge'] as const;

export function Table({ filter }: { filter: Filter }) {
  const select = useCallback((s: Snapshot) => groupsOf(s, filter, Date.now()), [filter]);
  const groups = useSnapshot<Group[]>(select).data ?? [];
  return (
    <div className={styles.tableWrap}>
      <table className={styles.table}>
        <thead>
          <tr>
            {COLUMNS.map((c) => (
              <th key={c} scope="col">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        {groups.map((g) => (
          <tbody key={g.key} className={g.stacked ? styles.stackGroup : undefined}>
            {g.stacked && (
              <tr className={styles.stackHeader}>
                <td colSpan={COLUMNS.length}>
                  <Pill tone="stack">Stack of {g.ids.length}</Pill>{' '}
                  <span className={styles.muted}>merge from the bottom up</span>
                </td>
              </tr>
            )}
            {g.ids.map((id) => (
              <Row key={id} id={id} />
            ))}
          </tbody>
        ))}
      </table>
      {groups.length === 0 && <p className={styles.empty}>No pull requests match.</p>}
    </div>
  );
}
