import { useId, useState } from 'react';
import {
  ApiError,
  type MergeMethodName,
  type MergeResult,
  type SnapshotPr,
  type SnapshotTicket,
  type Transition,
} from '@/api/client';
import { useMerge, useRepos, useSettings, useTicketTransitions } from '@/api/queries';
import {
  effectiveMethod,
  mergeBlock,
  mergeLabel,
  METHOD_NAMES,
  METHOD_ORDER,
  methodFrom,
  methodWord,
  type TicketChoice,
} from '@/lib/merge';
import { describeProblem } from '@/lib/problems';
import { refText } from '@/lib/rules';
import { blockOf } from '@/lib/snapshot';
import { Modal } from '../Modal/Modal';
import modal from '../Modal/Modal.module.css';
import { MenuItem, MenuNote, MenuTitle, PopMenu } from '../PopMenu/PopMenu';
import styles from './Dialogs.module.css';
import { ChevronDownIcon } from './Icons';

interface Draft {
  /** Set once the person opened the menu of the ticket, which is when its moves are loaded. */
  open: boolean;
  /** Null follows the default of the status sync rule, if any. */
  transitionId: string | null;
  fields: Record<string, string>;
  confirmed: boolean;
}

const UNTOUCHED: Draft = { open: false, transitionId: null, fields: {}, confirmed: false };
const MAX_TICKETS = 20;
const NONE = '';

/** The transition the first matching merged rule would make, when the rule is not waiting on other PRs. */
function ruleDefault(ticket: SnapshotTicket, list: Transition[]): string {
  const plan = ticket.mergePlan;
  if (!plan || plan.waitingOn.length > 0) return NONE;
  const target = plan.target.toLowerCase();
  return list.find((t) => t.toStatus.toLowerCase() === target)?.id ?? NONE;
}

/** "Jira KEY moves to [Done]": the status is a dropdown of the moves Jira offers, and "Leave as ...". */
function TicketLine({
  ticket,
  draft,
  transitionId,
  choice,
  transitions,
  onDraft,
}: {
  ticket: SnapshotTicket;
  draft: Draft;
  transitionId: string;
  choice: TicketChoice;
  transitions: ReturnType<typeof useTicketTransitions>[number];
  onDraft: (d: Draft) => void;
}) {
  const id = useId();
  const plan = ticket.mergePlan;
  const list = transitions.data?.transitions ?? [];
  const to = choice.transition;
  const current = ticket.status ?? 'its status';
  const shared = (t: Transition) => list.filter((x) => x.toStatus === t.toStatus).length > 1;
  const pick = (next: string, close: () => void) => {
    close();
    onDraft({ ...draft, open: true, transitionId: next, fields: {}, confirmed: false });
  };

  if (!ticket.found) {
    return <li>Jira {ticket.key}: Not found in Jira, it will not be moved</li>;
  }
  return (
    <li>
      Jira {ticket.key} {to ? 'moves to' : 'stays in'}{' '}
      <PopMenu
        label={`Move ${ticket.key}`}
        menuLabel={`Move ${ticket.key} to`}
        triggerClassName={styles.badge}
        trigger={
          <>
            {to ? to.toStatus : current}
            <ChevronDownIcon />
          </>
        }
        width="w48"
        side="above"
        onOpen={() => !draft.open && onDraft({ ...draft, open: true })}
      >
        {(close) => (
          <>
            <MenuTitle>Move ticket to</MenuTitle>
            {transitions.isPending && transitions.fetchStatus !== 'idle' && (
              <MenuNote>Loading moves</MenuNote>
            )}
            {list.map((t) => (
              <MenuItem
                key={t.id}
                active={t.id === transitionId}
                onSelect={() => pick(t.id, close)}
              >
                {shared(t) ? `${t.toStatus} (${t.name})` : t.toStatus}
              </MenuItem>
            ))}
            <MenuItem active={transitionId === NONE} onSelect={() => pick(NONE, close)}>
              Leave as {current}
            </MenuItem>
          </>
        )}
      </PopMenu>
      {plan && plan.waitingOn.length > 0 && (
        <div className={styles.note}>
          Won&apos;t move yet: waiting on {plan.waitingOn.map(refText).join(', ')}
        </div>
      )}
      {transitions.isError && (
        <div className={styles.error} role="alert">
          Could not load the moves of {ticket.key}.
        </div>
      )}
      {to && (
        <ul className={styles.sub}>
          {to.fields.map((f) => {
            const fieldId = `${id}-${f.id}`;
            return (
              <li key={f.id} className={styles.field}>
                <label htmlFor={fieldId}>
                  {f.name}
                  {f.required ? ' (required)' : ''}
                </label>
                {f.allowedValues.length > 0 ? (
                  <select
                    id={fieldId}
                    value={draft.fields[f.id]}
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
                    value={draft.fields[f.id]}
                    onChange={(e) =>
                      onDraft({ ...draft, fields: { ...draft.fields, [f.id]: e.target.value } })
                    }
                  />
                )}
              </li>
            );
          })}
          <li>
            <label className={styles.check}>
              <input
                type="checkbox"
                className={styles.box}
                checked={draft.confirmed}
                onChange={(e) => onDraft({ ...draft, confirmed: e.target.checked })}
              />
              Confirm moving {ticket.key} to {to.toStatus}
            </label>
          </li>
        </ul>
      )}
    </li>
  );
}

/**
 * The merge dialog. It lists what the merge does (the method, the head check, the branch, a stack,
 * each ticket and where it goes), asks for what Jira requires, and merges. It remembers the head
 * the card showed when it opened, and sends the merge only after the person confirmed every ticket
 * move or the choice to leave tickets alone. A refusal stays in the dialog as text; when the server
 * accepts, the dialog closes and `onMerged` starts the launch on the card.
 */
export function MergeDialog({
  pr,
  onClose,
  onMerged,
}: {
  pr: SnapshotPr;
  onClose: () => void;
  onMerged: (result: MergeResult) => void;
}) {
  const titleId = useId();
  const [headSha] = useState(pr.headSha);
  const [idempotencyKey, setKey] = useState(() => crypto.randomUUID());
  const [chosen, setChosen] = useState<MergeMethodName | null>(null);
  const [drafts, setDrafts] = useState<Record<string, Draft>>({});
  const [withoutTicket, setWithoutTicket] = useState(false);
  const [moved, setMoved] = useState(false);
  const [deleteChoice, setDeleteChoice] = useState<boolean | null>(null);
  const settings = useSettings();
  const repos = useRepos();
  const merge = useMerge();

  const method = effectiveMethod(pr.mergeMethod, chosen);
  const tickets = pr.tickets;
  const queries = useTicketTransitions(
    tickets.map((t) => ({
      key: t.key,
      enabled: drafts[t.key]?.open === true || t.mergePlan !== null,
    })),
  );
  const picked = tickets.map((t, i) => {
    const d = drafts[t.key] ?? UNTOUCHED;
    return d.transitionId ?? ruleDefault(t, queries[i]?.data?.transitions ?? []);
  });
  const choices: TicketChoice[] = tickets.map((t, i) => {
    const d = drafts[t.key] ?? UNTOUCHED;
    const transition = queries[i]?.data?.transitions.find((x) => x.id === picked[i]) ?? null;
    return { key: t.key, transition, fields: d.fields, confirmed: d.confirmed };
  });

  // The repository's own choice, else the default of the settings.
  const own = repos.data?.repos.find((r) => r.id === pr.repo.id)?.deleteBranch;
  const startDelete =
    own === 'yes' ? true : own === 'no' ? false : (settings.data?.deleteBranchAfterMerge ?? false);
  const deleteBranch = deleteChoice ?? startDelete;
  const rowBlock = blockOf(pr).reason;
  const reason =
    rowBlock ??
    (moved
      ? 'The pull request changed. Cancel and open this dialog again.'
      : mergeBlock(choices, withoutTicket));
  const label = mergeLabel(choices);
  const resolution = pr.mergeMethod;

  const send = () => {
    const movedTickets = choices.filter((c) => c.transition);
    const rest = choices.filter((c) => !c.transition);
    merge.mutate(
      {
        id: pr.id,
        key: idempotencyKey,
        pr,
        body: {
          method,
          expectedHeadSha: headSha,
          deleteBranch,
          tickets: [...movedTickets, ...rest].slice(0, MAX_TICKETS).map((c) => ({
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
        onSuccess: (result) => onMerged(result),
        onError: (e) => {
          if (e instanceof ApiError && e.code === 'head_moved') setMoved(true);
          if (e instanceof ApiError && e.status < 500) setKey(crypto.randomUUID());
        },
      },
    );
  };

  const error = merge.error;
  const stack = pr.stack;
  return (
    <Modal labelledBy={titleId} onClose={onClose} dismissible={!merge.isPending}>
      <h3 id={titleId} className={modal.title}>
        Merge {pr.repo.nameWithOwner} #{pr.number}?
      </h3>
      <p className={modal.subtitle}>{pr.title}</p>
      <ul className={styles.list}>
        <li>
          Method:{' '}
          <PopMenu
            label="Merge method"
            menuLabel="Merge method"
            triggerClassName={styles.badge}
            trigger={
              <>
                {methodWord(method)}
                <ChevronDownIcon />
              </>
            }
            width="w48"
            side="above"
          >
            {(close) => (
              <>
                <MenuTitle>Merge with</MenuTitle>
                {METHOD_ORDER.map((m) => {
                  const allowed = resolution.allowed.includes(m);
                  return (
                    <MenuItem
                      key={m}
                      active={m === method}
                      disabled={!allowed}
                      onSelect={() => {
                        close();
                        setChosen(m);
                      }}
                    >
                      {METHOD_NAMES[m]}
                      {!allowed ? ' (not allowed)' : m === resolution.method ? ' (default)' : ''}
                    </MenuItem>
                  );
                })}
              </>
            )}
          </PopMenu>
          {method === resolution.method
            ? `, from ${methodFrom(resolution.source)}`
            : ', chosen for this merge'}
          {resolution.warning && (
            <div className={`${styles.note} ${styles.warn}`}>{resolution.warning}</div>
          )}
        </li>
        <li>Head commit is checked again before merging</li>
        <li>
          <label className={styles.check}>
            {deleteBranch ? 'Branch is deleted after merge' : 'Branch is kept after merge'}
            <input
              type="checkbox"
              className={styles.box}
              aria-label="Delete branch after merge"
              checked={deleteBranch}
              onChange={(e) => setDeleteChoice(e.target.checked)}
            />
          </label>
        </li>
        {stack && (
          <li>
            Part of a stack, {stack.position} of {stack.size}. Merging it also merges the{' '}
            {stack.position > 1 ? 'PRs below it' : 'PR below it (none, this is the base)'}.
          </li>
        )}
        {tickets.length === 0 && <li>No Jira ticket to move</li>}
        {tickets.map((t, i) => {
          const query = queries[i];
          const choice = choices[i];
          return query && choice ? (
            <TicketLine
              key={t.key}
              ticket={t}
              draft={drafts[t.key] ?? UNTOUCHED}
              transitionId={picked[i] ?? NONE}
              choice={choice}
              transitions={query}
              onDraft={(d) => setDrafts((all) => ({ ...all, [t.key]: d }))}
            />
          ) : null;
        })}
        {choices.every((c) => !c.transition) && (
          <li>
            <label className={styles.check}>
              <input
                type="checkbox"
                className={styles.box}
                checked={withoutTicket}
                onChange={(e) => setWithoutTicket(e.target.checked)}
              />
              Merge without moving any ticket
            </label>
          </li>
        )}
      </ul>
      {error && (
        <p className={styles.error} role="alert">
          {describeProblem(
            error instanceof ApiError ? error.code : undefined,
            error instanceof ApiError ? error.detail : undefined,
          )}
        </p>
      )}
      <div className={modal.actions}>
        {reason && <span className={modal.hint}>{reason}</span>}
        <button type="button" className={modal.cancel} onClick={onClose} disabled={merge.isPending}>
          Cancel
        </button>
        <button
          type="button"
          className={styles.mergeBtn}
          disabled={reason !== null || merge.isPending}
          onClick={send}
        >
          {merge.isPending ? 'Merging' : label}
        </button>
      </div>
    </Modal>
  );
}
