import { useRecentMerges, useSnapshot } from '@/api/queries';
import styles from './CommandTable.module.css';
import { MergeSteps } from './MergeSteps';

/** Merges made in this tab whose row has left the table, so their outcome stays visible. */
export function RecentMerges() {
  const open = useSnapshot((s) => s.prs.map((p) => p.id)).data;
  const merges = useRecentMerges().data;
  const gone = (merges ?? []).filter((m) => open && !open.includes(m.prId));
  if (gone.length === 0) return null;
  return (
    <section className={styles.recent} aria-label="Recent merges">
      <h2>Recent merges</h2>
      {gone.map((m) => (
        <div key={m.prId}>
          <div>{m.label}</div>
          <MergeSteps steps={m.result.steps} />
        </div>
      ))}
    </section>
  );
}
