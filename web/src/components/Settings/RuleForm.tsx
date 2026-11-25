import { useId, useMemo, useState } from 'react';
import type { RuleFilters, RulePreview, RuleTrigger, SyncRule } from '@/api/client';
import { usePreviewRule, useSaveRules } from '@/api/queries';
import { withCurrent, MAX_HOURS, refText, ruleProblem, TRIGGERS } from '@/lib/rules';
import { Dropdown, type DropdownOption } from '../Dropdown/Dropdown';
import styles from './Settings.module.css';

type TextFilter = 'repo' | 'branch' | 'issueType';

const TEXT_FILTERS: Record<TextFilter, { label: string; placeholder: string }> = {
  repo: { label: 'Repository', placeholder: 'acme/web or acme/*' },
  branch: { label: 'Target branch', placeholder: 'main or release/*' },
  issueType: { label: 'Issue type', placeholder: 'Story' },
};

const ANY_PROJECT = 'Any project';
const ANY_STATUS = 'Any status';

const toOptions = (names: string[], first?: DropdownOption<string>): DropdownOption<string>[] => [
  ...(first ? [first] : []),
  ...names.map((n) => ({ value: n, label: n })),
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
  projects,
  onClose,
}: {
  initial: SyncRule;
  /** The saved rules in order. The edited one is replaced by id, a new one is appended. */
  others: SyncRule[];
  /** The Jira statuses to choose from. */
  statuses: string[];
  /** The Jira project keys to choose from. */
  projects: string[];
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

  const targetOptions = useMemo(() => {
    const first =
      rule.target === '' ? { value: '', label: 'Choose a status', disabled: true } : undefined;
    return toOptions(withCurrent(statuses, rule.target), first);
  }, [statuses, rule.target]);
  const statusOptions = useMemo(
    () => toOptions(withCurrent(statuses, rule.filters.status), { value: '', label: ANY_STATUS }),
    [statuses, rule.filters.status],
  );
  const projectOptions = useMemo(
    () => toOptions(withCurrent(projects, rule.filters.project), { value: '', label: ANY_PROJECT }),
    [projects, rule.filters.project],
  );
  const textFilter = (key: TextFilter) => (
    <label className={styles.stack}>
      {TEXT_FILTERS[key].label}
      <input
        type="text"
        placeholder={TEXT_FILTERS[key].placeholder}
        value={rule.filters[key]}
        onChange={(e) => setFilter({ [key]: e.target.value })}
      />
    </label>
  );
  const setFilter = (patch: Partial<RuleFilters>) =>
    set({ filters: { ...rule.filters, ...patch } });

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
        {textFilter('repo')}
        {textFilter('branch')}
        <div className={styles.stack}>
          <span>Jira project</span>
          <Dropdown
            label="Jira project"
            value={rule.filters.project}
            options={projectOptions}
            onChange={(project) => setFilter({ project })}
          />
        </div>
        {textFilter('issueType')}
        <div className={styles.stack}>
          <span>Current status</span>
          <Dropdown
            label="Current status"
            value={rule.filters.status}
            options={statusOptions}
            onChange={(status) => setFilter({ status })}
          />
        </div>
      </fieldset>
      <div className={styles.stack}>
        <span>Move the ticket to</span>
        <Dropdown
          label="Move the ticket to"
          value={rule.target}
          options={targetOptions}
          onChange={(target) => set({ target })}
        />
      </div>
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
