import type { Snapshot } from '@/api/client';
import { useSnapshot } from '@/api/queries';
import { safeLink } from '@/lib/snapshot';
import styles from './CommandTable.module.css';
import { LayersIcon } from './Icons';

interface StackFacts {
  tickets: { key: string; status: string | null; url: string | null }[];
  positions: number[];
  size: number;
}

/** The tickets across a stack (each key once, in stack order), the positions shown and the stack's size. */
export function stackFacts(s: Snapshot, ids: readonly string[]): StackFacts {
  const byId = new Map(s.prs.map((p) => [p.id, p]));
  const seen = new Map<string, StackFacts['tickets'][number]>();
  const positions: number[] = [];
  let size = ids.length;
  for (const id of ids) {
    const p = byId.get(id);
    if (!p) continue;
    if (p.stack) {
      positions.push(p.stack.position);
      size = p.stack.size;
    }
    for (const t of p.tickets) {
      if (!seen.has(t.key)) seen.set(t.key, { key: t.key, status: t.status, url: t.url });
    }
  }
  return { tickets: [...seen.values()], positions, size };
}

/** A ticket as a small outlined badge: "KEY · Status", a link to Jira when the link is valid. */
export function TicketBadge({
  ticketKey,
  status,
  url,
  title,
}: {
  ticketKey: string;
  status: string | null;
  url: string | null;
  /** The tooltip, such as the ticket's summary. */
  title?: string;
}) {
  const text = status ? `${ticketKey} · ${status}` : ticketKey;
  const link = safeLink(url);
  return link ? (
    <a
      className={styles.ticketBadge}
      href={link.href}
      target="_blank"
      rel="noopener noreferrer"
      title={title}
    >
      {text}
    </a>
  ) : (
    <span className={styles.ticketBadge} title={title}>
      {text}
    </span>
  );
}

/** The line above a stack: the STACK chip, its tickets, the merge hint and one step badge per member. */
export function StackBanner({ ids }: { ids: readonly string[] }) {
  const facts = useSnapshot((s) => stackFacts(s, ids)).data;
  if (!facts) return null;
  const n = facts.positions.length;
  return (
    <div className={styles.stackBanner}>
      <span className={styles.stackChip}>
        <LayersIcon />
        STACK
      </span>
      {facts.tickets.length > 0 && (
        <span className={styles.stackTickets}>
          {facts.tickets.map((t) => (
            <TicketBadge key={t.key} ticketKey={t.key} status={t.status} url={t.url} />
          ))}
        </span>
      )}
      <span className={styles.stackText}>
        {n === facts.size ? `${n} PRs, merge in order` : `${n} of ${facts.size} shown`}
      </span>
      <span className={styles.stackSteps}>
        {facts.positions.map((p) => (
          <span key={p} className={styles.stackStep}>
            {p}
          </span>
        ))}
      </span>
    </div>
  );
}
