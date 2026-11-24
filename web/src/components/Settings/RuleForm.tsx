import { useId, useState } from 'react';
import type { RuleFilters, RulePreview, RuleTrigger, SyncRule } from '@/api/client';
import { usePreviewRule, useSaveRules } from '@/api/queries';
import { MAX_HOURS, refText, ruleProblem, TRIGGERS } from '@/lib/rules';
import styles from './Settings.module.css';

const FILTERS: { key: keyof RuleFilters; label: string; placeholder: string }[] = [
  { key: 'repo', label: 'Repository', placeholder: 'acme/web or acme/*' },
  { key: 'branch', label: 'Target branch', placeholder: 'main or release/*' },
  { key: 'project', label: 'Jira project', placeholder: 'INP' },
  { key: 'issueType', label: 'Issue type', placeholder: 'Story' },
  { key: 'status', label: 'Current status', placeholder: 'In Review' },
];

function PreviewResult({ preview }: { preview: RulePreview }) {
  const moves = preview.moves.filter((m) => !m.alreadyInTarget);
  const there = preview.moves.filter((m) => m.alreadyInTarget);
  return (
    <div className={styles.preview} role="region" aria-label="Preview">
      {moves.length === 0 ? (
        <p>No ticket would move now.</p>
      ) : (
        <>
          <p>
            {moves.length === 1
              ? '1 ticket would move now:'
              : `${moves.length} tickets would move now:`}
          </p>
          <ul>
            {moves.map((m) => (
              <li key={m.ticketKey}>
                {m.ticketKey} ({m.status})
              </li>
            ))}
          </ul>
        </>
      )}
      {there.length > 0 && (
        <p className={styles.muted}>
          Already in the target status: {there.map((m) => m.ticketKey).join(', ')}.
        </p>
      )}
      {preview.waiting.length > 0 && (
        <ul aria-label="Waiting tickets">
          {preview.waiting.map((w) => (
            <li key={w.ticketKey} className={styles.muted}>
              {w.ticketKey}: waiting on {w.waitingOn.length} of {w.total} PRs:{' '}
              {w.waitingOn.map(refText).join(', ')}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Adds or edits one rule. Saving needs a preview of the rule as it stands, and a choice for the tickets that match now. */
export function RuleForm({
  initial,
  others,
  statuses,
  onClose,
}: {
  initial: SyncRule;
  /** The saved rules in order. The edited one is replaced by id, a new one is appended. */
  others: SyncRule[];
  statuses: string[];
  onClose: () => void;
}) {
  const id = useId();
  const [rule, setRule] = useState(initial);
  const [seen, setSeen] = useState<{ key: string; preview: RulePreview } | null>(null);
  const [keep, setKeep] = useState(false);
  const preview = usePreviewRule();
  const save = useSaveRules();

  const key = JSON.stringify(rule);
  const current = seen?.key === key ? seen.preview : null;
  const problem = ruleProblem(rule);
  const exists = others.some((r) => r.id === rule.id);
  const needsPreview = rule.enabled && current === null;

  const set = (patch: Partial<SyncRule>) => setRule({ ...rule, ...patch });
  const runPreview = () => preview.mutate(rule, { onSuccess: (p) => setSeen({ key, preview: p }) });
  const submit = () =>
    save.mutate(
      {
        rules: exists ? others.map((r) => (r.id === rule.id ? rule : r)) : [...others, rule],
        baseline: keep,
      },
      { onSuccess: onClose },
    );

  return (
    <form
      className={styles.form}
      aria-label={exists ? 'Edit rule' : 'New rule'}
      onSubmit={(e) => {
        e.preventDefault();
        if (!problem && !needsPreview) submit();
      }}
    >
      <label className={styles.stack}>
        Name (optional)
        <input
          type="text"
          value={rule.name}
          maxLength={100}
          onChange={(e) => set({ name: e.target.value })}
        />
      </label>
      <label className={styles.stack}>
        When
        <select
          value={rule.trigger}
          onChange={(e) => set({ trigger: e.target.value as RuleTrigger, hours: 0 })}
        >
          {TRIGGERS.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </select>
      </label>
      {rule.trigger === 'commit_stale' && (
        <label className={styles.stack}>
          Hours
          <input
            type="number"
            min={1}
            max={MAX_HOURS}
            value={rule.hours === 0 ? '' : rule.hours}
            onChange={(e) => set({ hours: e.target.value === '' ? 0 : Number(e.target.value) })}
          />
        </label>
      )}
      <fieldset className={styles.filters}>
        <legend>Only when (optional)</legend>
        {FILTERS.map((f) => (
          <label key={f.key} className={styles.stack}>
            {f.label}
            <input
              type="text"
              placeholder={f.placeholder}
              value={rule.filters[f.key]}
              onChange={(e) => set({ filters: { ...rule.filters, [f.key]: e.target.value } })}
            />
          </label>
        ))}
      </fieldset>
      <label className={styles.stack}>
        Move the ticket to
        <input
          type="text"
          list={`${id}-statuses`}
          placeholder="Done"
          value={rule.target}
          onChange={(e) => set({ target: e.target.value })}
        />
      </label>
      <datalist id={`${id}-statuses`}>
        {statuses.map((s) => (
          <option key={s} value={s} />
        ))}
      </datalist>
      <label className={styles.check}>
        <input
          type="checkbox"
          checked={rule.enabled}
          onChange={(e) => set({ enabled: e.target.checked })}
        />
        Enabled
      </label>
      {problem && (
        <p className={styles.error} role="alert">
          {problem}
        </p>
      )}
      {current && <PreviewResult preview={current} />}
      {current && current.moves.length > 0 && (
        <fieldset className={styles.filters}>
          <legend>Tickets that match now</legend>
          <label className={styles.check}>
            <input
              type="radio"
              name={`${id}-existing`}
              checked={!keep}
              onChange={() => setKeep(false)}
            />
            Move them
          </label>
          <label className={styles.check}>
            <input
              type="radio"
              name={`${id}-existing`}
              checked={keep}
              onChange={() => setKeep(true)}
            />
            Don&apos;t move existing tickets
          </label>
        </fieldset>
      )}
      {(preview.isError || save.isError) && (
        <p className={styles.error} role="alert">
          {preview.isError ? 'Could not preview the rule.' : 'Could not save the rules.'}
        </p>
      )}
      <div className={styles.actions}>
        <button type="button" disabled={problem !== null || preview.isPending} onClick={runPreview}>
          Preview
        </button>
        <button
          type="submit"
          className={styles.primary}
          disabled={problem !== null || needsPreview || save.isPending}
        >
          Save rule
        </button>
        <button type="button" onClick={onClose}>
          Cancel
        </button>
      </div>
      {needsPreview && !problem && (
        <p className={styles.muted}>Preview the rule before saving it.</p>
      )}
    </form>
  );
}
