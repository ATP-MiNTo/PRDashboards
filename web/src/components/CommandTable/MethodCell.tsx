import type { MergeMethodName, MethodResolution } from '@/api/client';
import { methodOptions, sourceHint } from '@/lib/merge';
import { Dropdown } from '../Dropdown/Dropdown';
import styles from './CommandTable.module.css';

export function MethodCell({
  resolution,
  value,
  onChange,
}: {
  resolution: MethodResolution;
  value: MergeMethodName;
  onChange: (m: MergeMethodName) => void;
}) {
  return (
    <div className={styles.method}>
      <Dropdown
        label="Merge method"
        value={value}
        options={methodOptions(resolution)}
        onChange={onChange}
      />
      <span className={styles.muted}>{sourceHint(resolution.source)}</span>
      {resolution.warning && <span className={styles.warning}>{resolution.warning}</span>}
    </div>
  );
}
