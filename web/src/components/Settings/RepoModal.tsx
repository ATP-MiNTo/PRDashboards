'use client';

import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import type {
  BranchRule,
  DeleteBranchChoice,
  MergeMethodName,
  RepoSettings,
  Snapshot,
} from '@/api/client';
import { useAccounts, useSaveRepo, useSettings, useSnapshot } from '@/api/queries';
import { SOURCE_VIA, sourceOf } from '@/lib/accounts';
import { METHOD_NAMES, METHOD_ORDER } from '@/lib/merge';
import {
  DELETE_CHOICES,
  MAX_APPROVALS,
  MAX_RULES,
  MIN_APPROVALS,
  allowedOf,
  branchesOf,
  deleteDefaultOf,
  deleteLabel,
  draftOf,
  draftValid,
  methodChoices,
  methodLabel,
  stepApprovals,
  updateOf,
  validPattern,
  withMethod,
  type ExplicitDelete,
  type MethodChoice,
  type RepoDraft,
} from '@/lib/repoSettings';
import { InfoTip } from '../InfoTip/InfoTip';
import { Modal } from '../Modal/Modal';
import { ModalHead } from '../Modal/ModalHead';
import { RollSwitch } from '../Toolbar/RollSwitch';
import { MinusIcon, PlusIcon, TrashIcon } from './icons';
import styles from './RepoModal.module.css';

/** Edits are saved this long after the last one, so a flick of the wheel is one request. */
export const REPO_SAVE_MS = 500;
/** The wheel steps the approvals at most this often. */
const WHEEL_STEP_MS = 120;

/** The approvals field: stepped by the wheel and the arrow keys, never typed into. */
function Approvals({ value, onStep }: { value: number; onStep: (dir: 1 | -1) => void }) {
  const input = useRef<HTMLInputElement>(null);
  const step = useRef(onStep);
  useEffect(() => {
    step.current = onStep;
  });
  useEffect(() => {
    const el = input.current;
    if (!el) return;
    let last = 0;
    // A native listener: React's wheel handler is passive and cannot stop the dialog from scrolling.
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      if (e.timeStamp - last < WHEEL_STEP_MS) return;
      last = e.timeStamp;
      step.current(e.deltaY < 0 ? 1 : -1);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);
  return (
    <div className={styles.join}>
      <input
        ref={input}
        type="text"
        inputMode="numeric"
        readOnly
        role="spinbutton"
        className={styles.count}
        aria-label="Approvals required"
        aria-valuenow={value}
        aria-valuemin={MIN_APPROVALS}
        aria-valuemax={MAX_APPROVALS}
        title="Scroll to change"
        value={value}
        onKeyDown={(e) => {
          if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
          e.preventDefault();
          onStep(e.key === 'ArrowUp' ? 1 : -1);
        }}
      />
      <button
        type="button"
        className={styles.step}
        aria-label="Fewer approvals"
        disabled={value <= MIN_APPROVALS}
        onClick={() => onStep(-1)}
      >
        <MinusIcon />
      </button>
      <button
        type="button"
        className={styles.step}
        aria-label="More approvals"
        disabled={value >= MAX_APPROVALS}
        onClick={() => onStep(1)}
      >
        <PlusIcon />
      </button>
    </div>
  );
}

function RuleRow({
  rule,
  index,
  listId,
  onChange,
  onRemove,
}: {
  rule: BranchRule;
  index: number;
  listId: string;
  onChange: (r: BranchRule) => void;
  onRemove: () => void;
}) {
  return (
    <li className={styles.rule}>
      <input
        type="text"
        className={styles.pattern}
        list={listId}
        placeholder="Search or pick a branch"
        aria-label={`Branch ${index + 1}`}
        aria-invalid={!validPattern(rule.pattern)}
        value={rule.pattern}
        onChange={(e) => onChange({ ...rule, pattern: e.target.value })}
      />
      <select
        className={styles.method}
        aria-label={`Method ${index + 1}`}
        value={rule.method}
        onChange={(e) => onChange({ ...rule, method: e.target.value as MergeMethodName })}
      >
        {METHOD_ORDER.map((m) => (
          <option key={m} value={m}>
            {METHOD_NAMES[m]}
          </option>
        ))}
      </select>
      <button
        type="button"
        className={styles.remove}
        title="Remove branch rule"
        aria-label={`Remove branch rule ${index + 1}`}
        onClick={onRemove}
      >
        <TrashIcon />
      </button>
    </li>
  );
}

const TIP_BRANCH =
  'Pick or type a branch. A pattern like release/* matches all. The first row that matches wins.';

/**
 * The settings of one repository, as a dialog: delete branch after merge, the merge method, the
 * approvals a pull request needs and the method per target branch. Changes are saved on their own
 * 0.5s after the last one (one `PUT /api/repos/{id}`, which rebuilds the snapshot from cached data)
 * and when the dialog closes, never while a branch pattern is invalid.
 */
export function RepoModal({ repo, onClose }: { repo: RepoSettings; onClose: () => void }) {
  const titleId = useId();
  const listId = useId();
  const settings = useSettings().data;
  const accounts = useAccounts().data;
  const allowed = useSnapshot(useCallback((s: Snapshot) => allowedOf(s, repo.id), [repo.id])).data;
  const known = useSnapshot(useCallback((s: Snapshot) => branchesOf(s, repo.id), [repo.id])).data;
  const save = useSaveRepo();
  const [draft, setDraft] = useState<RepoDraft>(() => draftOf(repo));
  const latest = useRef(draft);
  // What the server has, as far as this dialog knows: a draft that equals it is not sent again.
  const sent = useRef<string | null>(repo.isNew ? null : JSON.stringify(updateOf(draftOf(repo))));
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pending = useRef(false);
  const [failed, setFailed] = useState(false);
  // The delete-branch value the repository had last, for turning "Use default" off again. The server
  // keeps none while the choice is the default, so it starts from the stored choice when that is a value.
  const [ownDelete, setOwnDelete] = useState<ExplicitDelete | null>(
    repo.deleteBranch === 'default' ? null : repo.deleteBranch,
  );
  const { mutate } = save;
  const id = repo.id;

  const persist = useCallback(
    (force: boolean, closing = false) => {
      clearTimeout(timer.current);
      pending.current = false;
      let d = latest.current;
      if (!draftValid(d)) {
        // Edits wait for a valid pattern. Closing is the last chance, so the rows that are not valid go.
        if (!closing) return;
        d = { ...d, rules: d.rules.filter((r) => validPattern(r.pattern)).slice(0, MAX_RULES) };
      }
      const update = updateOf(d);
      const key = JSON.stringify(update);
      if (!force && key === sent.current) return;
      sent.current = key;
      mutate(
        { id, update },
        {
          onSuccess: () => setFailed(false),
          onError: () => {
            // Forget it, so the next change sends it again.
            sent.current = null;
            setFailed(true);
          },
        },
      );
    },
    [id, mutate],
  );

  const edit = (next: RepoDraft) => {
    latest.current = next;
    pending.current = true;
    setDraft(next);
    clearTimeout(timer.current);
    timer.current = setTimeout(() => persist(false), REPO_SAVE_MS);
  };

  // A dialog that unmounts with a change waiting (the repository went away, the page left) sends it.
  useEffect(
    () => () => {
      clearTimeout(timer.current);
      if (pending.current) persist(false, true);
    },
    [persist],
  );

  const close = () => {
    // A new repository is confirmed by closing its dialog.
    persist(pending.current || repo.isNew, true);
    onClose();
  };

  const globalDelete = settings?.deleteBranchAfterMerge;
  const globalMethod = settings?.defaultMethodForNewRepos;
  const setDelete = (deleteBranch: DeleteBranchChoice) => {
    if (draft.deleteBranch !== 'default') setOwnDelete(draft.deleteBranch);
    edit({ ...draft, deleteBranch });
  };
  const deleteOptions = DELETE_CHOICES.map((c) => ({
    id: c,
    label: deleteLabel(c, globalDelete),
  }));
  const methodOptions = useMemo(
    () =>
      methodChoices(allowed ?? { squash: true, merge: true, rebase: true }, draft.method).map(
        (c) => ({ id: c, label: methodLabel(c, globalMethod) }),
      ),
    [allowed, draft.method, globalMethod],
  );

  const source = accounts ? sourceOf(accounts.github) : null;
  const tip = `${source ? `Fetched via ${SOURCE_VIA[source]}. ` : ''}"Use default" follows the Repositories defaults.`;
  const valid = draftValid(draft);
  const setRules = (rules: BranchRule[]) => edit({ ...draft, rules });

  return (
    <Modal labelledBy={titleId} onClose={close}>
      <ModalHead
        titleId={titleId}
        title={repo.nameWithOwner}
        tip={tip}
        tipLabel="About this repository"
        closeLabel="Done"
        onClose={close}
      />
      {repo.isNew && (
        <p className={styles.note}>
          New repository. It starts from the default method and the seeded branch rules. Done
          confirms them.
        </p>
      )}
      <div className={styles.grid}>
        <div className={styles.field}>
          <span className={styles.label}>Delete branch after merge</span>
          <RollSwitch<DeleteBranchChoice>
            name="Delete branch after merge"
            variant="field"
            options={deleteOptions}
            value={draft.deleteBranch}
            defaultChoice={{
              id: 'default',
              resolvesTo: deleteDefaultOf(globalDelete),
              remembered: ownDelete ?? undefined,
            }}
            onChange={setDelete}
          />
        </div>
        <div className={styles.field}>
          <span className={styles.label}>Merge method</span>
          <RollSwitch<MethodChoice>
            name="Merge method"
            variant="field"
            options={methodOptions}
            value={draft.method}
            defaultChoice={{ id: 'default', resolvesTo: globalMethod, remembered: draft.ownMethod }}
            onChange={(method) => edit(withMethod(draft, method))}
          />
        </div>
        <div className={styles.field}>
          <span className={styles.label}>Approvals required to merge</span>
          <Approvals
            value={draft.approvals}
            onStep={(dir) => {
              const approvals = stepApprovals(latest.current.approvals, dir);
              if (approvals !== latest.current.approvals) edit({ ...latest.current, approvals });
            }}
          />
        </div>
      </div>
      <div className={styles.branches}>
        <div className={styles.eyebrow}>
          Method per target branch
          <InfoTip tip={TIP_BRANCH} label="About method per target branch" />
        </div>
        <datalist id={listId}>
          {(known ?? []).map((b) => (
            <option key={b} value={b} />
          ))}
        </datalist>
        {draft.rules.length > 0 ? (
          <ol className={styles.rules} aria-label="Method per target branch">
            {draft.rules.map((r, i) => (
              <RuleRow
                // Rows have no id and are only edited or removed, so the position is their key.
                key={i}
                rule={r}
                index={i}
                listId={listId}
                onChange={(next) => setRules(draft.rules.map((x, j) => (j === i ? next : x)))}
                onRemove={() => setRules(draft.rules.filter((_, j) => j !== i))}
              />
            ))}
          </ol>
        ) : (
          <p className={styles.empty}>No branch rules. Every branch uses the method above.</p>
        )}
        <button
          type="button"
          className={styles.add}
          disabled={draft.rules.length >= MAX_RULES}
          onClick={() =>
            setRules([
              ...draft.rules,
              { pattern: '', method: draft.method === 'default' ? 'squash' : draft.method },
            ])
          }
        >
          Add branch rule
        </button>
        {!valid && (
          <p className={styles.error} role="alert">
            Patterns use letters, digits and . _ / * - only, up to 200 characters. Rows that are not
            valid are not saved.
          </p>
        )}
        {failed && (
          <p className={styles.error} role="alert">
            Could not save the repository.
          </p>
        )}
      </div>
    </Modal>
  );
}
