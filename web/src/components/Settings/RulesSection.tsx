import { useRef, useState } from 'react';
import type { SyncRule } from '@/api/client';
import { useRules, useSaveRules, useSnapshot } from '@/api/queries';
import {
  describeFilters,
  MAX_RULES,
  modeLabel,
  newRule,
  ruleName,
  statusesOf,
  triggerLabel,
} from '@/lib/rules';
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

export function RulesSection() {
  const rules = useRules();
  const save = useSaveRules();
  const statuses = useSnapshot(statusesOf).data ?? [];
  const [form, setForm] = useState<SyncRule | null>(null);
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
      <h3 id="set-rules">Status sync rules</h3>
      <p className={styles.muted}>
        The first enabled rule that matches a ticket moves it, once, when something new happens on
        its pull requests.
      </p>
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
      {form ? (
        <RuleForm
          key={form.id}
          initial={form}
          others={list}
          statuses={statuses}
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
