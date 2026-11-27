import { useMemo, useRef, useState } from 'react';
import type { SyncRule } from '@/api/client';
import {
  useJiraOptions,
  useRules,
  useSaveRules,
  useSaveSettings,
  useSettings,
  useSnapshot,
} from '@/api/queries';
import {
  describeFilters,
  MAX_RULES,
  modeLabel,
  newRule,
  preferList,
  projectsOf,
  ruleName,
  statusesOf,
  statusKey,
  triggerLabel,
} from '@/lib/rules';
import { MultiSelect } from '../MultiSelect/MultiSelect';
import { RuleForm } from './RuleForm';
import styles from './Settings.module.css';

function summary(r: SyncRule): string {
  const when =
    r.trigger === 'commit_stale'
      ? `The latest commit is older than ${r.hours} hours`
      : triggerLabel(r.trigger);
  const filters = describeFilters(r.filters);
  return `${when}, then move to ${r.target}${filters.length ? ` (${filters.join(', ')})` : ''}`;
}

const MAX_EXCLUDED = 50;

/** A searchable multi-select of the statuses whose tickets no rule moves. A status that is excluded but unknown to Jira stays listed. */
function ExcludeStatuses({
  statuses,
  excluded,
  pending,
  onChange,
  onOpen,
}: {
  statuses: string[];
  excluded: string[];
  pending: boolean;
  onChange: (next: string[]) => void;
  onOpen: () => void;
}) {
  const { names, values } = useMemo(() => {
    // Near-duplicates (by `statusKey`) count as one: an excluded "reopen" selects the listed "Reopen".
    const byKey = new Map<string, string>();
    for (const n of [...statuses, ...excluded]) {
      const key = statusKey(n);
      if (!byKey.has(key)) byKey.set(key, n);
    }
    const picked = new Set(excluded.map((e) => byKey.get(statusKey(e)) ?? e));
    return {
      names: [...byKey.values()],
      values: [...picked],
    };
  }, [statuses, excluded]);
  return (
    <div className={styles.group} role="group" aria-label="Exclude statuses">
      <b>Exclude statuses</b>
      <p className={styles.hint}>Tickets in these statuses are never moved.</p>
      <MultiSelect
        label="Exclude statuses"
        values={values}
        options={names}
        max={MAX_EXCLUDED}
        disabled={pending}
        onChange={onChange}
        onOpen={onOpen}
      />
    </div>
  );
}

export function RulesSection() {
  const rules = useRules();
  const save = useSaveRules();
  const settings = useSettings();
  const saveSettings = useSaveSettings();
  const [form, setForm] = useState<SyncRule | null>(null);
  // Jira statuses and projects cost a server-side Jira call, so they load only when the rule form
  // opens or the Exclude statuses list is first opened, never on mount.
  const [wantOptions, setWantOptions] = useState(false);
  const options = useJiraOptions(wantOptions || form !== null);
  const snapshotStatuses = useSnapshot(statusesOf).data;
  const snapshotProjects = useSnapshot(projectsOf).data;
  const statuses = useMemo(
    () => preferList(options.data?.statuses, snapshotStatuses ?? []),
    [options.data, snapshotStatuses],
  );
  const projects = useMemo(
    () => preferList(options.data?.projects, snapshotProjects ?? []),
    [options.data, snapshotProjects],
  );
  const [dragging, setDragging] = useState<number | null>(null);
  const from = useRef<number | null>(null);
  const list = rules.data?.rules ?? [];

  const put = (next: SyncRule[]) => save.mutate({ rules: next, baseline: false });
  const move = (a: number, b: number) => {
    if (a === b || b < 0 || b >= list.length) return;
    const next = [...list];
    const [item] = next.splice(a, 1);
    if (item) next.splice(b, 0, item);
    put(next);
  };
  const toggle = (r: SyncRule, enabled: boolean) => {
    // Enabling a rule evaluates it against the current tickets, so it goes through the preview.
    if (enabled) setForm({ ...r, enabled });
    else put(list.map((x) => (x.id === r.id ? { ...x, enabled } : x)));
  };

  return (
    <section className={styles.section} aria-labelledby="set-rules">
      <h3 id="set-rules" tabIndex={-1}>
        Status sync rules
      </h3>
      {rules.isPending && <p className={styles.muted}>Loading rules</p>}
      {rules.isError && (
        <p className={styles.error} role="alert">
          Could not load the rules.
        </p>
      )}
      {rules.isSuccess && list.length === 0 && !form && (
        <p className={styles.muted}>No rules yet.</p>
      )}
      <ol className={styles.rulesList} aria-label="Status sync rules">
        {list.map((r, i) => (
          <li
            key={r.id}
            className={dragging === i ? `${styles.ruleItem} ${styles.dragging}` : styles.ruleItem}
            draggable
            onDragStart={(e) => {
              from.current = i;
              setDragging(i);
              if (e.dataTransfer) {
                e.dataTransfer.effectAllowed = 'move';
                e.dataTransfer.setData('text/plain', r.id);
              }
            }}
            onDragOver={(e) => {
              if (from.current !== null) e.preventDefault();
            }}
            onDrop={(e) => {
              e.preventDefault();
              if (from.current !== null) move(from.current, i);
              from.current = null;
              setDragging(null);
            }}
            onDragEnd={() => {
              from.current = null;
              setDragging(null);
            }}
          >
            <input
              type="checkbox"
              aria-label={`Enable ${ruleName(r)}`}
              checked={r.enabled}
              disabled={save.isPending}
              onChange={(e) => toggle(r, e.target.checked)}
            />
            <div className={styles.ruleMain}>
              <b>{ruleName(r)}</b>
              <span className={styles.muted}>
                {summary(r)}. Mode {modeLabel(r.mode)}.
              </span>
            </div>
            <div className={styles.actions}>
              <button
                type="button"
                aria-label={`Move ${ruleName(r)} up`}
                disabled={i === 0 || save.isPending}
                onClick={() => move(i, i - 1)}
              >
                Up
              </button>
              <button
                type="button"
                aria-label={`Move ${ruleName(r)} down`}
                disabled={i === list.length - 1 || save.isPending}
                onClick={() => move(i, i + 1)}
              >
                Down
              </button>
              <button type="button" aria-label={`Edit ${ruleName(r)}`} onClick={() => setForm(r)}>
                Edit
              </button>
              <button
                type="button"
                aria-label={`Delete ${ruleName(r)}`}
                disabled={save.isPending}
                onClick={() => put(list.filter((x) => x.id !== r.id))}
              >
                Delete
              </button>
            </div>
          </li>
        ))}
      </ol>
      {save.isError && !form && (
        <p className={styles.error} role="alert">
          Could not save the rules.
        </p>
      )}
      {settings.data && (
        <ExcludeStatuses
          statuses={statuses}
          excluded={settings.data.excludedStatuses}
          pending={saveSettings.isPending}
          onOpen={() => setWantOptions(true)}
          onChange={(excludedStatuses) =>
            saveSettings.mutate({ ...settings.data, excludedStatuses })
          }
        />
      )}
      {saveSettings.isError && (
        <p className={styles.error} role="alert">
          Could not save the excluded statuses.
        </p>
      )}
      {form ? (
        <RuleForm
          key={form.id}
          initial={form}
          others={list}
          statuses={statuses}
          projects={projects}
          onClose={() => setForm(null)}
        />
      ) : (
        <div className={styles.actions}>
          <button
            type="button"
            disabled={list.length >= MAX_RULES || !rules.isSuccess}
            onClick={() => setForm(newRule())}
          >
            Add rule
          </button>
        </div>
      )}
    </section>
  );
}
