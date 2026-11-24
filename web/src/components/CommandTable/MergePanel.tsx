import { useId, useState } from 'react';
import { ApiError, type MergeMethodName, type SnapshotPr, type SnapshotTicket } from '@/api/client';
import { useMerge, useRecentMerges, useSettings, useTicketTransitions } from '@/api/queries';
import { mergeBlock, mergeLabel, METHOD_NAMES, type TicketChoice } from '@/lib/merge';
import { describeProblem } from '@/lib/problems';
import { blockOf } from '@/lib/snapshot';
import { Dropdown } from '../Dropdown/Dropdown';
import styles from './CommandTable.module.css';
import { MergeSteps } from './MergeSteps';

interface Draft {
  open: boolean;
  transitionId: string;
  fields: Record<string, string>;
  confirmed: boolean;
}

const UNTOUCHED: Draft = { open: false, transitionId: '', fields: {}, confirmed: false };
const MAX_TICKETS = 20;
const NONE = '';

function TicketMove({
  ticket,
  draft,
  choice,
  transitions,
  onDraft,
}: {
  ticket: SnapshotTicket;
  draft: Draft;
  choice: TicketChoice;
  transitions: ReturnType<typeof useTicketTransitions>[number];
  onDraft: (d: Draft) => void;
}) {
  const id = useId();
  const list = transitions.data?.transitions ?? [];
  const options = [
    { value: NONE, label: "Don't move" },
    ...list.map((t) => ({ value: t.id, label: `Move to ${t.toStatus}` })),
  ];
  return (
    <li className={styles.moveRow}>
      <div>
        <span className={styles.ticketKey}>{ticket.key}</span>{' '}
        <span className={styles.muted}>{ticket.summary ?? ''}</span>
        {ticket.status && <span className={styles.muted}> ({ticket.status})</span>}
      </div>
      {!ticket.found ? (
        <span className={styles.muted}>Not found in Jira, it will not be moved</span>
      ) : !draft.open ? (
        <div className={styles.moveChoice}>
          <span>Don&apos;t move</span>
          <button
            type="button"
            className={styles.linkButton}
            aria-label={`Move ${ticket.key}`}
            onClick={() => onDraft({ ...draft, open: true })}
          >
            Move...
          </button>
        </div>
      ) : transitions.isPending ? (
        <span className={styles.muted}>Loading moves</span>
      ) : transitions.isError ? (
        <span className={styles.errorText} role="alert">
          Could not load the moves of {ticket.key}.
        </span>
      ) : (
        <div className={styles.moveChoice}>
          <Dropdown
            label={`Move ${ticket.key}`}
            value={draft.transitionId}
            options={options}
            onChange={(transitionId) =>
              onDraft({ ...draft, transitionId, fields: {}, confirmed: false })
            }
          />
          {choice.transition?.fields.map((f) => {
            const fieldId = `${id}-${f.id}`;
            return (
              <div key={f.id} className={styles.field}>
                <label htmlFor={fieldId}>
                  {f.name}
                  {f.required ? ' (required)' : ''}
                </label>
                {f.allowedValues.length > 0 ? (
                  <select
                    id={fieldId}
                    value={draft.fields[f.id] ?? ''}
                    onChange={(e) =>
                      onDraft({ ...draft, fields: { ...draft.fields, [f.id]: e.target.value } })
                    }
                  >
                    <option value="">Choose</option>
                    {f.allowedValues.map((v) => (
                      <option key={v.id} value={v.id}>
                        {v.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    id={fieldId}
                    type="text"
                    value={draft.fields[f.id] ?? ''}
                    onChange={(e) =>
                      onDraft({ ...draft, fields: { ...draft.fields, [f.id]: e.target.value } })
                    }
                  />
                )}
              </div>
            );
          })}
          {choice.transition && (
            <label className={styles.checkbox}>
              <input
                type="checkbox"
                checked={draft.confirmed}
                onChange={(e) => onDraft({ ...draft, confirmed: e.target.checked })}
              />
              Confirm moving {ticket.key} to {choice.transition.toStatus}
            </label>
          )}
        </div>
      )}
    </li>
  );
}

export function MergePanel({
  pr,
  method,
  onClose,
}: {
  pr: SnapshotPr;
  method: MergeMethodName;
  onClose: () => void;
}) {
  const [headSha] = useState(pr.headSha);
  const [idempotencyKey, setKey] = useState(() => crypto.randomUUID());
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [withoutTicket, setWithoutTicket] = useState(false);
  const [moved, setMoved] = useState(false);
  const [deleteChoice, setDeleteChoice] = useState<boolean | null>(null);
  const settings = useSettings();
  const merge = useMerge();
  const done = useRecentMerges((all) => all.find((m) => m.prId === pr.id)).data;

  const tickets = pr.tickets;
  const queries = useTicketTransitions(
    tickets.map((t) => ({ key: t.key, enabled: drafts[t.key]?.open === true })),
  );
  const choices: TicketChoice[] = tickets.map((t, i) => {
    const d = drafts[t.key] ?? UNTOUCHED;
    const transition = queries[i]?.data?.transitions.find((x) => x.id === d.transitionId) ?? null;
    return { key: t.key, transition, fields: d.fields, confirmed: d.confirmed };
  });

  const deleteBranch = deleteChoice ?? settings.data?.deleteBranchAfterMerge ?? false;
  const rowBlock = blockOf(pr).reason;
  const formBlock = mergeBlock(choices, withoutTicket);
  const reason =
    rowBlock ?? (moved ? 'The pull request changed. Cancel and open this panel again.' : formBlock);
  const label = mergeLabel(choices);

  const send = () => {
    const moved = choices.filter((c) => c.transition);
    const rest = choices.filter((c) => !c.transition);
    merge.mutate(
      {
        id: pr.id,
        key: idempotencyKey,
        label: `${pr.repo.nameWithOwner}#${pr.number} ${pr.title}`,
        body: {
          method,
          expectedHeadSha: headSha,
          deleteBranch,
          tickets: [...moved, ...rest].slice(0, MAX_TICKETS).map((c) => ({
            key: c.key,
            transitionId: c.transition?.id ?? null,
            fields: Object.fromEntries(
              Object.entries(c.fields).filter(
                ([id, v]) => v.trim() !== '' && c.transition?.fields.some((f) => f.id === id),
              ),
            ),
          })),
        },
      },
      {
        onError: (e) => {
          if (e instanceof ApiError && e.code === 'head_moved') setMoved(true);
          if (e instanceof ApiError && e.status < 500) setKey(crypto.randomUUID());
        },
      },
    );
  };

  if (done) {
    return (
      <div className={styles.mergePanel} aria-label={`Merge result of #${pr.number}`}>
        <MergeSteps steps={done.result.steps} />
        <button type="button" className={styles.linkButton} onClick={onClose}>
          Close
        </button>
      </div>
    );
  }

  const error = merge.error;
  return (
    <div className={styles.mergePanel} role="group" aria-label={`Merge #${pr.number}`}>
      <div className={styles.mergeLine}>
        <span>
          Method: <b>{METHOD_NAMES[method]}</b>
        </span>
        <label className={styles.checkbox}>
          <input
            type="checkbox"
            checked={deleteBranch}
            onChange={(e) => setDeleteChoice(e.target.checked)}
          />
          Delete branch after merge
        </label>
      </div>
      {tickets.length > 0 && (
        <ul className={styles.moves} aria-label="Tickets">
          {tickets.map((t, i) => {
            const query = queries[i];
            const choice = choices[i];
            return query && choice ? (
              <TicketMove
                key={t.key}
                ticket={t}
                draft={drafts[t.key] ?? UNTOUCHED}
                choice={choice}
                transitions={query}
                onDraft={(d) => setDrafts((all) => ({ ...all, [t.key]: d }))}
              />
            ) : null;
          })}
        </ul>
      )}
      {choices.every((c) => !c.transition) && (
        <label className={styles.checkbox}>
          <input
            type="checkbox"
            checked={withoutTicket}
            onChange={(e) => setWithoutTicket(e.target.checked)}
          />
          Merge without moving any ticket
        </label>
      )}
      {error && (
        <p className={styles.errorText} role="alert">
          {describeProblem(error instanceof ApiError ? error.code : undefined)}
        </p>
      )}
      <div className={styles.mergeLine}>
        <button
          type="button"
          className={styles.mergeButton}
          disabled={reason !== null || merge.isPending}
          onClick={send}
        >
          {merge.isPending ? 'Merging' : label}
        </button>
        <button type="button" className={styles.linkButton} onClick={onClose}>
          Cancel
        </button>
        {reason && <span className={styles.reason}>{reason}</span>}
      </div>
    </div>
  );
}
