import { useId, useMemo, useState } from 'react';
import type { BranchRule, MergeMethodName, RepoSettings } from '@/api/client';
import { useRepos, useSaveRepo, useSaveSettings, useSettings, useSnapshot } from '@/api/queries';
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

function RepoPanel({ repo }: { repo: RepoSettings }) {
  const id = useId();
  const [open, setOpen] = useState(repo.isNew);
  const [method, setMethod] = useState(repo.defaultMethod);
  const [rules, setRules] = useState(repo.branchRules);
  const save = useSaveRepo();
  const branches = useSnapshot((s) => branchesOf(s, repo.id)).data ?? [];

  const changed =
    method !== repo.defaultMethod || JSON.stringify(rules) !== JSON.stringify(repo.branchRules);
  const valid = rules.length <= MAX_RULES && rules.every((r) => validPattern(r.pattern));
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
              onChange={setMethod}
            />
          </div>
          <h4>Branch rules</h4>
          <p className={styles.muted}>The first rule that matches the base branch wins.</p>
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
          <div className={styles.actions}>
            <button
              type="button"
              disabled={rules.length >= MAX_RULES}
              onClick={() => setRules([...rules, { pattern: '', method }])}
            >
              Add rule to {repo.nameWithOwner}
            </button>
            <button
              type="button"
              className={styles.primary}
              disabled={!valid || (!changed && !repo.isNew) || save.isPending}
              onClick={() =>
                save.mutate({
                  id: repo.id,
                  update: { defaultMethod: method, branchRules: rules, clearNew: true },
                })
              }
            >
              Save {repo.nameWithOwner}
            </button>
          </div>
          {!valid && (
            <p className={styles.error} role="alert">
              Patterns use letters, digits and . _ / * - only, up to 200 characters.
            </p>
          )}
          {save.isError && (
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

export function RepositoriesSection() {
  const repos = useRepos();
  const [query, setQuery] = useState('');
  const q = query.trim().toLowerCase();
  const shown = useMemo(
    () => (repos.data?.repos ?? []).filter((r) => r.nameWithOwner.toLowerCase().includes(q)),
    [repos.data, q],
  );
  return (
    <section className={styles.section} aria-labelledby="set-repos">
      <h3 id="set-repos">Repositories</h3>
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
        {shown.map((r) => (
          <RepoPanel key={`${r.id}-${JSON.stringify(r)}`} repo={r} />
        ))}
      </ul>
    </section>
  );
}
