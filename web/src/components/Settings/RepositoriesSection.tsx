import { useId, useMemo, useState } from 'react';
import type { BranchRule, MergeMethodName, RepoSettings, RepoUpdate } from '@/api/client';
import {
  useRepos,
  useSaveRepo,
  useSaveRepos,
  useSaveSettings,
  useSettings,
  useSnapshot,
} from '@/api/queries';
import { METHOD_CHOICES } from '@/lib/merge';
import type { Snapshot } from '@/api/client';
import { Pill } from '../CommandTable/Pill';
import { Dropdown } from '../Dropdown/Dropdown';
import styles from './Settings.module.css';

export const MAX_RULES = 50;
const PATTERN = /^[A-Za-z0-9._/*-]{1,200}$/;

export function validPattern(p: string): boolean {
  return PATTERN.test(p);
}

/** The branches the snapshot knows for a repository, as suggestions for a rule pattern. */
export function branchesOf(s: Snapshot, repoId: string): string[] {
  const names = new Set<string>();
  for (const pr of s.prs) {
    if (pr.repo.id !== repoId) continue;
    names.add(pr.baseRef);
    names.add(pr.headRef);
  }
  return [...names].sort();
}

function RuleRow({
  rule,
  index,
  count,
  listId,
  repoName,
  onChange,
  onMove,
  onRemove,
}: {
  rule: BranchRule;
  index: number;
  count: number;
  listId: string;
  repoName: string;
  onChange: (r: BranchRule) => void;
  onMove: (to: number) => void;
  onRemove: () => void;
}) {
  const n = index + 1;
  return (
    <li className={styles.rule}>
      <input
        type="text"
        list={listId}
        aria-label={`Branch pattern ${n} of ${repoName}`}
        aria-invalid={!validPattern(rule.pattern)}
        placeholder="main or release/*"
        value={rule.pattern}
        onChange={(e) => onChange({ ...rule, pattern: e.target.value })}
      />
      <Dropdown<MergeMethodName>
        label={`Method for rule ${n} of ${repoName}`}
        value={rule.method}
        options={METHOD_CHOICES}
        onChange={(method) => onChange({ ...rule, method })}
      />
      <button
        type="button"
        aria-label={`Move rule ${n} of ${repoName} up`}
        disabled={index === 0}
        onClick={() => onMove(index - 1)}
      >
        Up
      </button>
      <button
        type="button"
        aria-label={`Move rule ${n} of ${repoName} down`}
        disabled={index === count - 1}
        onClick={() => onMove(index + 1)}
      >
        Down
      </button>
      <button type="button" aria-label={`Remove rule ${n} of ${repoName}`} onClick={onRemove}>
        Remove
      </button>
    </li>
  );
}

/** The unsaved edits of one repository. */
interface Draft {
  method: MergeMethodName;
  rules: BranchRule[];
}

const draftOf = (r: RepoSettings): Draft => ({ method: r.defaultMethod, rules: r.branchRules });
const draftValid = (d: Draft) =>
  d.rules.length <= MAX_RULES && d.rules.every((r) => validPattern(r.pattern));
const draftChanged = (d: Draft, r: RepoSettings) =>
  d.method !== r.defaultMethod || JSON.stringify(d.rules) !== JSON.stringify(r.branchRules);
const toUpdate = (d: Draft): RepoUpdate => ({
  defaultMethod: d.method,
  branchRules: d.rules,
  clearNew: true,
});

function RepoPanel({
  repo,
  draft,
  changed,
  valid,
  saving,
  failed,
  onEdit,
  onSave,
}: {
  repo: RepoSettings;
  draft: Draft;
  changed: boolean;
  valid: boolean;
  saving: boolean;
  failed: boolean;
  onEdit: (next: Draft) => void;
  onSave: () => void;
}) {
  const id = useId();
  const [open, setOpen] = useState(repo.isNew);
  const branches = useSnapshot((s) => branchesOf(s, repo.id)).data ?? [];
  const { method, rules } = draft;

  const setRules = (next: BranchRule[]) => onEdit({ method, rules: next });
  const edit = (i: number, r: BranchRule) => setRules(rules.map((x, j) => (j === i ? r : x)));
  const move = (from: number, to: number) => {
    const next = [...rules];
    const [item] = next.splice(from, 1);
    if (item) next.splice(to, 0, item);
    setRules(next);
  };

  return (
    <li className={styles.repo}>
      <button
        type="button"
        className={styles.repoHead}
        aria-expanded={open}
        aria-controls={`${id}-panel`}
        onClick={() => setOpen(!open)}
      >
        <span>{repo.nameWithOwner}</span>
        {repo.isNew && <Pill tone="info">New</Pill>}
        <span className={styles.muted}>{repo.openPrs} open</span>
      </button>
      {open && (
        <div id={`${id}-panel`} className={styles.repoBody}>
          {repo.isNew && (
            <p className={styles.note}>
              New repository. It starts from the default for new repositories and the seeded branch
              rules. Save to confirm them.
            </p>
          )}
          <div className={styles.field}>
            <span>Default method</span>
            <Dropdown<MergeMethodName>
              label={`Default method of ${repo.nameWithOwner}`}
              value={method}
              options={METHOD_CHOICES}
              onChange={(m) => onEdit({ method: m, rules })}
            />
          </div>
          <h4>Branch rules</h4>
          <datalist id={`${id}-branches`}>
            {branches.map((b) => (
              <option key={b} value={b} />
            ))}
          </datalist>
          <ol className={styles.rules} aria-label={`Branch rules of ${repo.nameWithOwner}`}>
            {rules.map((r, i) => (
              <RuleRow
                key={i}
                rule={r}
                index={i}
                count={rules.length}
                listId={`${id}-branches`}
                repoName={repo.nameWithOwner}
                onChange={(next) => edit(i, next)}
                onMove={(to) => move(i, to)}
                onRemove={() => setRules(rules.filter((_, j) => j !== i))}
              />
            ))}
          </ol>
          <div className={styles.actions} role="group" aria-label={repo.nameWithOwner}>
            <button
              type="button"
              disabled={rules.length >= MAX_RULES}
              onClick={() => setRules([...rules, { pattern: '', method }])}
            >
              Add rule
            </button>
            <button
              type="button"
              className={styles.primary}
              disabled={!valid || (!changed && !repo.isNew) || saving}
              onClick={onSave}
            >
              Save
            </button>
          </div>
          {!valid && (
            <p className={styles.error} role="alert">
              Patterns use letters, digits and . _ / * - only, up to 200 characters.
            </p>
          )}
          {failed && (
            <p className={styles.error} role="alert">
              Could not save the repository.
            </p>
          )}
        </div>
      )}
    </li>
  );
}

export function DefaultMethod() {
  const settings = useSettings();
  const save = useSaveSettings();
  const current = settings.data;
  if (!current) return null;
  return (
    <div className={styles.field}>
      <span>Default for new repositories</span>
      <Dropdown<MergeMethodName>
        label="Default for new repositories"
        value={current.defaultMethodForNewRepos}
        options={METHOD_CHOICES}
        onChange={(m) => save.mutate({ ...current, defaultMethodForNewRepos: m })}
      />
    </div>
  );
}

const repoIds = (s: Snapshot) => [...new Set(s.prs.map((p) => p.repo.id))];

export function RepositoriesSection() {
  const repos = useRepos();
  const saveOne = useSaveRepo();
  const saveAll = useSaveRepos();
  const [expanded, setExpanded] = useState(false);
  const [query, setQuery] = useState('');
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [failed, setFailed] = useState<string | null>(null);
  const panelId = useId();
  const list = repos.data?.repos;
  const withPrs = useSnapshot(repoIds).data;
  const inList = list && withPrs ? list.filter((r) => withPrs.includes(r.id)).length : null;
  const q = query.trim().toLowerCase();
  const shown = useMemo(
    () => (list ?? []).filter((r) => r.nameWithOwner.toLowerCase().includes(q)),
    [list, q],
  );

  // Only repositories with a draft are compared, so an untouched list costs nothing.
  const states = useMemo(() => {
    const out = new Map<string, { changed: boolean; valid: boolean }>();
    for (const r of list ?? []) {
      const d = drafts[r.id];
      out.set(
        r.id,
        d ? { changed: draftChanged(d, r), valid: draftValid(d) } : { changed: false, valid: true },
      );
    }
    return out;
  }, [list, drafts]);

  const toSave = (list ?? []).filter((r) => states.get(r.id)?.changed || r.isNew);
  const saving = saveOne.isPending || saveAll.isPending;
  const canSaveAll = toSave.length > 0 && !saving && toSave.every((r) => states.get(r.id)?.valid);

  const forget = (ids: string[]) =>
    setDrafts((prev) => {
      const next = { ...prev };
      for (const id of ids) delete next[id];
      return next;
    });
  const draftFor = (r: RepoSettings) => drafts[r.id] ?? draftOf(r);

  const save = (r: RepoSettings) => {
    setFailed(null);
    saveOne.mutate(
      { id: r.id, update: toUpdate(draftFor(r)) },
      { onSuccess: () => forget([r.id]), onError: () => setFailed(r.id) },
    );
  };
  const saveEvery = () => {
    setFailed(null);
    saveAll.mutate(
      { repos: toSave.map((r) => ({ id: r.id, update: toUpdate(draftFor(r)) })) },
      { onSuccess: () => forget(toSave.map((r) => r.id)), onError: () => setFailed('all') },
    );
  };

  return (
    <section className={styles.section} aria-labelledby="set-repos">
      <h3 id="set-repos" className={styles.foldHead}>
        <button
          type="button"
          className={styles.foldButton}
          aria-expanded={expanded}
          aria-controls={panelId}
          onClick={() => setExpanded(!expanded)}
        >
          <span className={styles.foldLabel}>
            <span className={styles.eyebrow}>Repositories setting</span>
            {inList !== null && (
              <span
                className={styles.count}
                title={`${inList} of ${list?.length ?? 0} repositories have open pull requests`}
              >
                {inList}/{list?.length ?? 0}
              </span>
            )}
          </span>
          <svg
            className={styles.chevron}
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            focusable="false"
          >
            <path d="m9 6 6 6-6 6" />
          </svg>
        </button>
      </h3>
      {expanded && (
        <div id={panelId} className={styles.group}>
          <DefaultMethod />
          <input
            type="search"
            aria-label="Search repositories"
            placeholder="Search repositories"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {repos.isPending && <p className={styles.muted}>Loading repositories</p>}
          {repos.isError && (
            <p className={styles.error} role="alert">
              Could not load the repositories.
            </p>
          )}
          {repos.isSuccess && shown.length === 0 && (
            <p className={styles.muted}>{q ? 'No repository matches.' : 'No repositories yet.'}</p>
          )}
          <ul className={styles.repos}>
            {shown.map((r) => {
              const st = states.get(r.id);
              return (
                <RepoPanel
                  key={r.id}
                  repo={r}
                  draft={draftFor(r)}
                  changed={st?.changed ?? false}
                  valid={st?.valid ?? true}
                  saving={saving}
                  failed={failed === r.id}
                  onEdit={(next) => setDrafts((prev) => ({ ...prev, [r.id]: next }))}
                  onSave={() => save(r)}
                />
              );
            })}
          </ul>
          {repos.isSuccess && (list?.length ?? 0) > 0 && (
            <div className={styles.actions}>
              <button
                type="button"
                className={styles.primary}
                disabled={!canSaveAll}
                onClick={saveEvery}
              >
                Save all
              </button>
            </div>
          )}
          {failed === 'all' && (
            <p className={styles.error} role="alert">
              Could not save the repositories.
            </p>
          )}
        </div>
      )}
    </section>
  );
}
