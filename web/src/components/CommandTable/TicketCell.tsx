import { useState } from 'react';
import type { SnapshotPr, SnapshotTicket } from '@/api/client';
import { useAssignedTickets, useSaveTickets, useSyncNow } from '@/api/queries';
import { safeLink } from '@/lib/snapshot';
import styles from './CommandTable.module.css';
import { Pill, type Tone } from './Pill';

const COLLAPSED = 2;
const MAX_MANUAL = 20;

const SOURCE: Record<SnapshotTicket['source'], string> = {
  branch: 'from branch',
  title: 'from title',
  manual: 'added manually',
};

function tone(category: string | null): Tone {
  if (category === 'done') return 'good';
  if (category === 'indeterminate') return 'info';
  return 'mute';
}

function Ticket({
  ticket,
  lookup,
  onUnlink,
  busy,
}: {
  ticket: SnapshotTicket;
  lookup: SnapshotPr['ticketLookup'];
  onUnlink: () => void;
  busy: boolean;
}) {
  const link = safeLink(ticket.url);
  return (
    <li className={styles.ticket}>
      <div className={styles.ticketHead}>
        {link ? (
          <a
            className={styles.ticketKey}
            href={link.href}
            target="_blank"
            rel="noopener noreferrer"
          >
            {ticket.key}
          </a>
        ) : (
          <span className={styles.ticketKey}>{ticket.key}</span>
        )}
        {ticket.status ? <Pill tone={tone(ticket.statusCategory)}>{ticket.status}</Pill> : null}
        <button
          type="button"
          className={styles.unlink}
          aria-label={`Unlink ${ticket.key}`}
          title={`Unlink ${ticket.key}`}
          disabled={busy}
          onClick={onUnlink}
        >
          ×
        </button>
      </div>
      {ticket.found ? (
        <div className={styles.ticketTitle}>{ticket.summary}</div>
      ) : (
        <div className={styles.muted}>
          {lookup === 'ok'
            ? 'Not found in Jira'
            : lookup === 'unverified'
              ? 'Not checked in Jira'
              : 'Not loaded'}
        </div>
      )}
      <div className={styles.source}>{SOURCE[ticket.source]}</div>
    </li>
  );
}

function Picker({
  pr,
  onPick,
  onClose,
}: {
  pr: SnapshotPr;
  onPick: (key: string) => void;
  onClose: () => void;
}) {
  const [query, setQuery] = useState('');
  const assigned = useAssignedTickets(true);
  const linked = new Set(pr.tickets.map((t) => t.key));
  const q = query.trim().toLowerCase();
  const options = (assigned.data?.tickets ?? []).filter(
    (t) =>
      !linked.has(t.key) &&
      (q === '' || t.key.toLowerCase().includes(q) || t.summary.toLowerCase().includes(q)),
  );
  return (
    <div className={styles.picker}>
      <input
        type="search"
        aria-label="Search your tickets"
        placeholder="Search your tickets"
        value={query}
        autoFocus
        onChange={(e) => setQuery(e.target.value)}
      />
      {assigned.isPending && <p className={styles.muted}>Loading tickets</p>}
      {assigned.isError && <p className={styles.errorText}>Could not load your tickets.</p>}
      {assigned.isSuccess && options.length === 0 && (
        <p className={styles.muted}>No matching tickets</p>
      )}
      <ul className={styles.options} aria-label="Assigned tickets">
        {options.map((t) => (
          <li key={t.key}>
            <button type="button" className={styles.option} onClick={() => onPick(t.key)}>
              <span className={styles.ticketKey}>{t.key}</span> <span>{t.summary}</span>
            </button>
          </li>
        ))}
      </ul>
      <button type="button" className={styles.linkButton} onClick={onClose}>
        Close
      </button>
    </div>
  );
}

export function TicketCell({ pr }: { pr: SnapshotPr }) {
  const [expanded, setExpanded] = useState(false);
  const [picking, setPicking] = useState(false);
  const save = useSaveTickets();
  const sync = useSyncNow();

  const manual = pr.tickets.filter((t) => t.source === 'manual').map((t) => t.key);
  const apply = (nextManual: string[], nextRemoved: string[]) =>
    save.mutate({ id: pr.id, links: { manual: nextManual, removed: nextRemoved } });
  const unlink = (t: SnapshotTicket) =>
    t.source === 'manual'
      ? apply(
          manual.filter((k) => k !== t.key),
          pr.removedKeys,
        )
      : apply(manual, [...pr.removedKeys, t.key]);
  const add = (key: string) => {
    setPicking(false);
    apply(
      [...manual, key],
      pr.removedKeys.filter((k) => k !== key),
    );
  };

  const shown = expanded ? pr.tickets : pr.tickets.slice(0, COLLAPSED);
  const hidden = pr.tickets.length - shown.length;

  return (
    <div className={styles.tickets}>
      {pr.ticketLookup === 'unreachable' && (
        <div className={styles.lookupError} role="alert">
          <span>Jira unreachable, retry the lookup</span>
          <button
            type="button"
            className={styles.linkButton}
            disabled={sync.isPending}
            onClick={() => sync.mutate()}
          >
            Retry lookup
          </button>
        </div>
      )}
      {pr.tickets.length === 0 && <span className={styles.muted}>No ticket</span>}
      {shown.length > 0 && (
        <ul className={styles.ticketList}>
          {shown.map((t) => (
            <Ticket
              key={t.key}
              ticket={t}
              lookup={pr.ticketLookup}
              busy={save.isPending}
              onUnlink={() => unlink(t)}
            />
          ))}
        </ul>
      )}
      {hidden > 0 && (
        <button type="button" className={styles.linkButton} onClick={() => setExpanded(true)}>
          +{hidden} more
        </button>
      )}
      {expanded && pr.tickets.length > COLLAPSED && (
        <button type="button" className={styles.linkButton} onClick={() => setExpanded(false)}>
          Show less
        </button>
      )}
      {save.isError && (
        <p className={styles.errorText} role="alert">
          Could not save the change.
        </p>
      )}
      {picking ? (
        <Picker pr={pr} onPick={add} onClose={() => setPicking(false)} />
      ) : (
        manual.length < MAX_MANUAL && (
          <button type="button" className={styles.linkButton} onClick={() => setPicking(true)}>
            Link ticket
          </button>
        )
      )}
    </div>
  );
}
