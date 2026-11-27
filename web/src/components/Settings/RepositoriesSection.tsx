import { useId, useMemo, useState } from 'react';
import type { MergeMethodName, RepoSettings } from '@/api/client';
import { useAccounts, useRepos, useSaveSettings, useSettings } from '@/api/queries';
import { SOURCE_SHORT, sourceOf } from '@/lib/accounts';
import { METHOD_NAMES, METHOD_ORDER } from '@/lib/merge';
import { isCustom } from '@/lib/repoSettings';
import { InfoTip } from '../InfoTip/InfoTip';
import { Marquee } from '../Marquee/Marquee';
import { RollSwitch } from '../Toolbar/RollSwitch';
import { GearIcon, GearOnIcon } from './icons';
import { RepoModal } from './RepoModal';
import styles from './Settings.module.css';

const DELETE_OPTIONS = [
  { id: 'yes', label: 'Yes' },
  { id: 'no', label: 'No' },
] as const;

const TIP_DELETE =
  'Yes: branches are deleted after a merge. No: branches are kept. Each repository can have its own choice. Click the button to switch to the next state.';
const TIP_REPOSITORY =
  'Blue gear: this repository has its own settings. Outline gear: it follows the defaults above.';

/** The defaults every repository follows until it says otherwise: delete branch and the merge method. */
function Defaults() {
  const settings = useSettings();
  const save = useSaveSettings();
  const current = settings.data;
  const methodId = useId();
  if (!current) return null;
  return (
    <div className={styles.defaults}>
      <div className={styles.deleteRow}>
        <span className={styles.deleteLabel}>Delete branch after merge</span>
        <span className={styles.tip}>
          <InfoTip tip={TIP_DELETE} label="About Delete branch after merge" />
        </span>
        <div className={styles.deleteRoll}>
          <RollSwitch<'yes' | 'no'>
            name="Delete branch after merge, default"
            options={DELETE_OPTIONS}
            value={current.deleteBranchAfterMerge ? 'yes' : 'no'}
            onChange={(v) => save.mutate({ ...current, deleteBranchAfterMerge: v === 'yes' })}
          />
        </div>
      </div>
      <div>
        <label className={styles.fieldLabel} htmlFor={methodId}>
          Default merge method
        </label>
        <select
          id={methodId}
          value={current.defaultMethodForNewRepos}
          onChange={(e) =>
            save.mutate({
              ...current,
              defaultMethodForNewRepos: e.target.value as MergeMethodName,
            })
          }
        >
          {METHOD_ORDER.map((m) => (
            <option key={m} value={m}>
              {METHOD_NAMES[m]}
            </option>
          ))}
        </select>
      </div>
      {save.isError && (
        <p className={styles.error} role="alert">
          Could not save the settings.
        </p>
      )}
    </div>
  );
}

function RepoRow({
  repo,
  shown,
  via,
  onToggle,
  onGear,
}: {
  repo: RepoSettings;
  shown: boolean;
  via: string | null;
  onToggle: () => void;
  onGear: () => void;
}) {
  const custom = isCustom(repo);
  return (
    <li className={styles.repo}>
      <label className={styles.repoLabel}>
        <input
          type="checkbox"
          className={styles.repoCheck}
          checked={shown}
          aria-label={`Show ${repo.nameWithOwner}`}
          onChange={onToggle}
        />
        <Marquee className={styles.repoName}>{repo.nameWithOwner}</Marquee>
        {via && <span className={styles.via}>{via}</span>}
      </label>
      <button
        type="button"
        className={custom ? `${styles.gear} ${styles.gearOn}` : styles.gear}
        aria-haspopup="dialog"
        aria-label={`Settings for ${repo.nameWithOwner}`}
        title={custom ? 'Custom settings' : 'Uses defaults'}
        onClick={onGear}
      >
        {custom ? <GearOnIcon /> : <GearIcon />}
      </button>
    </li>
  );
}

/**
 * The Repositories block of the sidebar: a fold header (the eyebrow, how many repositories the list
 * shows and a chevron), and when open the defaults, Select all and Clear, and one row per
 * repository with a checkbox that shows or hides its pull requests, the way GitHub is connected
 * (the VIA badge) and a gear that opens the repository's settings dialog. A blue gear means the
 * repository has settings of its own.
 */
export function RepositoriesSection({
  off,
  onOff,
}: {
  /** Ids of the repositories whose pull requests the list leaves out. */
  off: ReadonlySet<string>;
  onOff: (next: Set<string>) => void;
}) {
  const repos = useRepos();
  const accounts = useAccounts().data;
  const [expanded, setExpanded] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const panelId = useId();
  const list = useMemo(() => repos.data?.repos ?? [], [repos.data]);
  const shown = list.filter((r) => !off.has(r.id)).length;
  const source = accounts ? sourceOf(accounts.github) : null;
  const via = source ? SOURCE_SHORT[source] : null;
  const repo = editing === null ? undefined : list.find((r) => r.id === editing);

  const toggle = (id: string) => {
    const next = new Set(off);
    if (!next.delete(id)) next.add(id);
    onOff(next);
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
            {repos.isSuccess && (
              <span
                className={styles.count}
                title={`${shown} of ${list.length} repositories are shown in the list`}
              >
                {shown}/{list.length}
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
        <div id={panelId} className={styles.open}>
          <Defaults />
          <div className={styles.listHead}>
            <span className={styles.listTitle}>
              <span className={styles.dim}>Repository</span>
              <span className={styles.tip}>
                <InfoTip tip={TIP_REPOSITORY} label="About Repository" />
              </span>
            </span>
            <div className={styles.join} role="group" aria-label="Show repositories">
              <button
                type="button"
                className={styles.joinBtn}
                disabled={shown === list.length}
                onClick={() => onOff(new Set())}
              >
                Select all
              </button>
              <button
                type="button"
                className={styles.joinBtn}
                disabled={shown === 0}
                onClick={() => onOff(new Set(list.map((r) => r.id)))}
              >
                Clear
              </button>
            </div>
          </div>
          {repos.isPending && <p className={styles.muted}>Loading repositories</p>}
          {repos.isError && (
            <p className={styles.error} role="alert">
              Could not load the repositories.
            </p>
          )}
          {repos.isSuccess && list.length === 0 && (
            <p className={styles.muted}>No repositories yet.</p>
          )}
          <ul className={styles.repos} aria-label="Repositories">
            {list.map((r) => (
              <RepoRow
                key={r.id}
                repo={r}
                shown={!off.has(r.id)}
                via={via}
                onToggle={() => toggle(r.id)}
                onGear={() => setEditing(r.id)}
              />
            ))}
          </ul>
        </div>
      )}
      {repo && <RepoModal key={repo.id} repo={repo} onClose={() => setEditing(null)} />}
    </section>
  );
}
