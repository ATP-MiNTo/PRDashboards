import { useId, useMemo, useState, type DragEvent } from 'react';
import type { RuleFilters, RuleMode, RulePreview, RuleTrigger, SyncRule } from '@/api/client';
import { usePreviewRule, useSaveRules, useSettings } from '@/api/queries';
import {
  MAX_HOURS,
  modeChoices,
  MULTI_PR_HINT,
  ruleName,
  ruleProblem,
  TRIGGERS,
  triggerLabel,
  withCurrent,
} from '@/lib/rules';
import { CHIP_LABELS, previewKey, ruleChanged, sameRule } from '@/lib/rulesEditor';
import { Tip } from '../Tip/Tip';
import { ChipField } from './ChipField';
import { PreviewResult } from './PreviewResult';
import styles from './RulesModal.module.css';

const GRIP = (
  <svg width="14" height="18" viewBox="0 0 14 18" fill="currentColor" aria-hidden="true">
    <circle cx="4" cy="4" r="1.6" />
    <circle cx="10" cy="4" r="1.6" />
    <circle cx="4" cy="9" r="1.6" />
    <circle cx="10" cy="9" r="1.6" />
    <circle cx="4" cy="14" r="1.6" />
    <circle cx="10" cy="14" r="1.6" />
  </svg>
);

const ICON = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
} as const;

const TRASH = (
  <svg width="16" height="16" viewBox="0 0 24 24" {...ICON}>
    <path d="M3 6h18" />
    <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
    <path d="M10 11v6M14 11v6" />
    <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
  </svg>
);

const CHEVRON = (
  <svg width="16" height="16" viewBox="0 0 24 24" className={styles.chevron} {...ICON}>
    <path d="m6 9 6 6 6-6" />
  </svg>
);

export interface CardDrag {
  /** The grip was pressed: the card may now be dragged. */
  onGrip: (card: HTMLElement) => void;
  onStart: (e: DragEvent<HTMLElement>, index: number) => void;
  onEnd: () => void;
}

export interface RuleCardProps {
  index: number;
  /** The rule as shown: the saved one with the edits of the dialog on it. */
  rule: SyncRule;
  /** The saved rule, null for a new one. */
  saved: SyncRule | null;
  /** The saved rules in order, for saving this one into them. */
  others: SyncRule[];
  collapsed: boolean;
  dragging: boolean;
  first: boolean;
  last: boolean;
  /** A change of the list is being saved. */
  busy: boolean;
  statuses: string[];
  projects: string[];
  repos: string[];
  branches: string[];
  drag: CardDrag;
  onEdit: (patch: Partial<SyncRule>) => void;
  onEnabled: (on: boolean) => void;
  onMode: (mode: RuleMode) => void;
  onFold: () => void;
  onDelete: () => void;
  onMove: (dir: -1 | 1) => void;
  onSaved: () => void;
  onDiscard: () => void;
}

const options = (names: readonly string[]) =>
  names.map((n) => (
    <option key={n} value={n}>
      {n}
    </option>
  ));

/** One rule: its head (grip, number, name, mode, switch, trash, fold) and its collapsible body. */
export function RuleCard({
  index,
  rule,
  saved,
  others,
  collapsed,
  dragging,
  first,
  last,
  busy,
  statuses,
  projects,
  repos,
  branches,
  drag,
  onEdit,
  onEnabled,
  onMode,
  onFold,
  onDelete,
  onMove,
  onSaved,
  onDiscard,
}: RuleCardProps) {
  const id = useId();
  const [seen, setSeen] = useState<{ key: string; preview: RulePreview } | null>(null);
  const [keep, setKeep] = useState(false);
  const preview = usePreviewRule();
  const save = useSaveRules();
  const multiPr = useSettings().data?.multiPrTickets ?? 'all';

  const name = ruleName(rule);
  const key = previewKey(rule);
  const current = seen?.key === key ? seen.preview : null;
  const problem = ruleProblem(rule);
  const pending = saved === null || !sameRule(saved, rule);
  const needsPreview =
    rule.enabled && (saved === null || ruleChanged(saved, rule)) && current === null;

  const targets = useMemo(() => withCurrent(statuses, rule.target), [statuses, rule.target]);
  const setFilter = (patch: Partial<RuleFilters>) =>
    onEdit({ filters: { ...rule.filters, ...patch } });
  const filterValue = (k: keyof RuleFilters) => (rule.filters[k] ? [rule.filters[k]] : []);
  const chip = (k: keyof RuleFilters) => ({
    label: CHIP_LABELS[k],
    values: filterValue(k),
    single: true,
    onAdd: (v: string) => setFilter({ [k]: v }),
    onRemove: () => setFilter({ [k]: '' }),
  });

  const submit = () =>
    save.mutate(
      {
        rules: saved ? others.map((r) => (r.id === rule.id ? rule : r)) : [...others, rule],
        baseline: keep && current !== null && current.moves.length > 0,
      },
      { onSuccess: onSaved },
    );
  const runPreview = () => preview.mutate(rule, { onSuccess: (p) => setSeen({ key, preview: p }) });

  const cls = [styles.card, collapsed ? styles.collapsed : '', rule.enabled ? '' : styles.off]
    .filter(Boolean)
    .join(' ');

  return (
    <li
      className={dragging ? `${cls} ${styles.dragging}` : cls}
      data-rule-card={rule.id}
      aria-label={`Rule ${index + 1}, ${name}`}
      onDragStart={saved ? (e) => drag.onStart(e, index) : undefined}
      onDragEnd={drag.onEnd}
    >
      <div className={styles.head} data-rule-head>
        <span
          className={saved ? styles.grip : `${styles.grip} ${styles.gripOff}`}
          title="Drag to reorder"
          aria-hidden="true"
          onMouseDown={(e) => saved && drag.onGrip(e.currentTarget.closest('li') as HTMLElement)}
        >
          {GRIP}
        </span>
        <span className={styles.badge} title="Rules run top to bottom">
          {index + 1}
        </span>
        <input
          type="text"
          className={styles.nameInput}
          aria-label="Rule name"
          maxLength={100}
          placeholder={ruleName({ ...rule, name: '' })}
          value={rule.name}
          onChange={(e) => onEdit({ name: e.target.value })}
        />
        {pending && <span className={styles.unsaved}>Unsaved</span>}
        <select
          className={`${styles.selectXs} ${styles.mode}`}
          aria-label="Mode"
          value={rule.enabled ? 'auto' : 'off'}
          disabled={busy}
          onChange={(e) => onEnabled(e.target.value === 'auto')}
        >
          <option value="auto">Auto</option>
          <option value="off">Off</option>
        </select>
        <input
          type="checkbox"
          className={styles.switch}
          aria-label={`Enable ${name}`}
          title={rule.enabled ? 'On' : 'Off'}
          checked={rule.enabled}
          disabled={busy}
          onChange={(e) => onEnabled(e.target.checked)}
        />
        <button
          type="button"
          className={`${styles.iconBtn} ${styles.trash}`}
          title="Remove rule"
          aria-label={`Delete ${name}`}
          disabled={busy}
          onClick={onDelete}
        >
          {TRASH}
        </button>
        <button
          type="button"
          className={`${styles.iconBtn} ${styles.fold}`}
          aria-expanded={!collapsed}
          aria-controls={`${id}-body`}
          aria-label={`${collapsed ? 'Expand' : 'Collapse'} ${name}`}
          onClick={onFold}
        >
          {CHEVRON}
        </button>
        {saved && (
          <>
            <button
              type="button"
              className={styles.keyMove}
              aria-label={`Move ${name} up`}
              disabled={first || busy}
              onClick={() => onMove(-1)}
            >
              Up
            </button>
            <button
              type="button"
              className={styles.keyMove}
              aria-label={`Move ${name} down`}
              disabled={last || busy}
              onClick={() => onMove(1)}
            >
              Down
            </button>
          </>
        )}
      </div>
      <div id={`${id}-body`} className={styles.body} inert={collapsed}>
        <div className={styles.bodyInner} data-rule-body>
          <div className={styles.bodyPad}>
            <div className={styles.grid}>
              <div className={styles.field} role="group" aria-label="When this happens to the PR">
                <span className={styles.label}>When this happens to the PR</span>
                <div className={styles.chips}>
                  <span className={styles.chip}>{triggerLabel(rule.trigger)}</span>
                  <select
                    className={styles.selectXs}
                    aria-label="When"
                    value=""
                    onChange={(e) =>
                      e.target.value && onEdit({ trigger: e.target.value as RuleTrigger, hours: 0 })
                    }
                  >
                    <option value="">Change</option>
                    {TRIGGERS.filter((t) => t.value !== rule.trigger).map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </select>
                </div>
              </div>
              <ChipField
                {...chip('branch')}
                placeholder="main or release/*"
                suggestions={branches}
              />
              <ChipField {...chip('repo')} placeholder="acme/web or acme/*" suggestions={repos} />
              <ChipField {...chip('project')} options={projects} />
              <ChipField {...chip('status')} options={statuses} />
              <ChipField {...chip('issueType')} placeholder="Story" />
              {rule.trigger === 'commit_stale' && (
                <div className={styles.field} role="group" aria-label="If the latest commit is">
                  <span className={styles.label}>If the latest commit is</span>
                  <div className={styles.chips}>
                    <span className={styles.chip}>Older than</span>
                    <input
                      type="number"
                      className={`${styles.inputXs} ${styles.hours}`}
                      aria-label="Hours"
                      min={1}
                      max={MAX_HOURS}
                      value={rule.hours === 0 ? '' : rule.hours}
                      onChange={(e) =>
                        onEdit({ hours: e.target.value === '' ? 0 : Number(e.target.value) })
                      }
                    />
                    <span>hours</span>
                  </div>
                </div>
              )}
            </div>
            <div className={styles.bottom}>
              <label className={styles.control}>
                <span className={styles.label}>Then move it to</span>
                <select
                  className={styles.selectSm}
                  value={rule.target}
                  onChange={(e) => onEdit({ target: e.target.value })}
                >
                  {rule.target === '' && (
                    <option value="" disabled>
                      Choose a status
                    </option>
                  )}
                  {options(targets)}
                </select>
              </label>
              <div className={styles.control}>
                <span className={styles.label}>Tickets shared by several PRs</span>
                <Tip tip={MULTI_PR_HINT} side="top">
                  <select
                    className={styles.selectSm}
                    aria-label="Tickets shared by several PRs"
                    aria-describedby={`${id}-hint`}
                    value={rule.mode}
                    disabled={busy}
                    onChange={(e) => onMode(e.target.value as RuleMode)}
                  >
                    {modeChoices(multiPr).map((m) => (
                      <option key={m.value} value={m.value}>
                        {m.label}
                      </option>
                    ))}
                  </select>
                </Tip>
                <span id={`${id}-hint`} className={styles.sr}>
                  {MULTI_PR_HINT}
                </span>
              </div>
            </div>
            {pending && (
              <div className={styles.pending} role="group" aria-label={`Save ${name}`}>
                {problem && (
                  <p className={styles.error} role="alert">
                    {problem}
                  </p>
                )}
                {current && <PreviewResult preview={current} />}
                {current && current.moves.length > 0 && (
                  <fieldset className={styles.choice}>
                    <legend>Tickets that match now</legend>
                    <label className={styles.radio}>
                      <input
                        type="radio"
                        name={`${id}-existing`}
                        checked={!keep}
                        onChange={() => setKeep(false)}
                      />
                      Move them
                    </label>
                    <label className={styles.radio}>
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
                  <button
                    type="button"
                    className={styles.outline}
                    disabled={problem !== null || preview.isPending}
                    onClick={runPreview}
                  >
                    Preview
                  </button>
                  <button
                    type="button"
                    className={styles.primary}
                    disabled={problem !== null || needsPreview || save.isPending}
                    onClick={submit}
                  >
                    Save rule
                  </button>
                  <button type="button" className={styles.outline} onClick={onDiscard}>
                    Cancel
                  </button>
                </div>
                {needsPreview && !problem && (
                  <p className={styles.muted}>Preview the rule before saving it.</p>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
    </li>
  );
}
