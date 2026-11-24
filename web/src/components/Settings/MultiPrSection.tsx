import { useId } from 'react';
import type { RuleMode, SyncRule } from '@/api/client';
import { useRules, useSaveRules } from '@/api/queries';
import { modeLabel, MULTI_PR_HINT, ruleName } from '@/lib/rules';
import styles from './Settings.module.css';

function Chip({
  rule,
  busy,
  onMode,
}: {
  rule: SyncRule;
  busy: boolean;
  onMode: (mode: RuleMode) => void;
}) {
  const name = ruleName(rule);
  return (
    <button
      type="button"
      className={styles.chip}
      aria-pressed={rule.mode === 'all'}
      aria-label={`${name}: ${modeLabel(rule.mode)}`}
      disabled={busy}
      onClick={() => onMode('all')}
      onContextMenu={(e) => {
        e.preventDefault();
        onMode('any');
      }}
      onKeyDown={(e) => {
        if (e.key !== 'Enter') return;
        e.preventDefault();
        onMode(e.shiftKey ? 'any' : 'all');
      }}
    >
      <span>{name}</span>
      <b>{modeLabel(rule.mode)}</b>
    </button>
  );
}

export function MultiPrSection() {
  const hint = useId();
  const rules = useRules();
  const save = useSaveRules();
  const list = rules.data?.rules ?? [];
  // Changing the mode does not move tickets that already match: the new state becomes the baseline.
  const setMode = (id: string, mode: RuleMode) => {
    if (list.find((r) => r.id === id)?.mode === mode) return;
    save.mutate({ rules: list.map((r) => (r.id === id ? { ...r, mode } : r)), baseline: true });
  };
  return (
    <section className={styles.section} aria-labelledby="set-multi">
      <div className={styles.titleWithHint}>
        <h3 id="set-multi">Multi-PR tickets</h3>
        <span className={styles.hintWrap}>
          <button
            type="button"
            className={styles.info}
            aria-label="About multi-PR tickets"
            aria-describedby={hint}
          >
            ⓘ
          </button>
          <span id={hint} role="tooltip" className={styles.tip}>
            {MULTI_PR_HINT}
          </span>
        </span>
      </div>
      {rules.isSuccess && list.length === 0 && (
        <p className={styles.muted}>Add a status sync rule to choose its mode.</p>
      )}
      <ul className={styles.chips} aria-label="Mode of each rule">
        {list.map((r) => (
          <li key={r.id}>
            <Chip rule={r} busy={save.isPending} onMode={(m) => setMode(r.id, m)} />
          </li>
        ))}
      </ul>
      {save.isError && (
        <p className={styles.error} role="alert">
          Could not save the mode.
        </p>
      )}
    </section>
  );
}
