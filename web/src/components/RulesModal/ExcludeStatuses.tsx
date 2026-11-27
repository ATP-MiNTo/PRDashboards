import { useMemo } from 'react';
import { useSaveSettings, useSettings } from '@/api/queries';
import { statusKey } from '@/lib/rules';
import { ChipField } from './ChipField';
import styles from './RulesModal.module.css';

/** The most statuses the settings take. */
export const MAX_EXCLUDED = 50;

/**
 * The statuses whose tickets no rule moves, as chips. Near-duplicates (by `statusKey`) count as one:
 * an excluded "reopen" selects the listed "Reopen", and a status that Jira does not list stays.
 */
export function ExcludeStatuses({ statuses }: { statuses: string[] }) {
  const settings = useSettings();
  const save = useSaveSettings();
  const excluded = settings.data?.excludedStatuses;
  const { names, values } = useMemo(() => {
    const byKey = new Map<string, string>();
    for (const n of [...statuses, ...(excluded ?? [])]) {
      const key = statusKey(n);
      if (!byKey.has(key)) byKey.set(key, n);
    }
    const picked = new Set((excluded ?? []).map((e) => byKey.get(statusKey(e)) ?? e));
    return { names: [...byKey.values()], values: [...picked] };
  }, [statuses, excluded]);
  if (!settings.data) return null;
  const current = settings.data;
  const put = (next: string[]) => save.mutate({ ...current, excludedStatuses: next });
  return (
    <section className={styles.section} aria-label="Exclude statuses">
      <div className={styles.eyebrow}>Exclude statuses</div>
      <ChipField
        label="Tickets in these statuses are never moved."
        values={values}
        options={values.length >= MAX_EXCLUDED ? [] : names}
        addLabel="Add excluded status"
        onAdd={(v) => put([...values, v])}
        onRemove={(v) => put(values.filter((x) => statusKey(x) !== statusKey(v)))}
      />
      {save.isError && (
        <p className={styles.error} role="alert">
          Could not save the excluded statuses.
        </p>
      )}
    </section>
  );
}
