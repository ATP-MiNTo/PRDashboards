'use client';

import { useRecentMerges, useRetryTransition } from '@/api/queries';
import { initials } from '@/lib/card';
import {
  historyEntry,
  historyUrl,
  matchesHistory,
  mergedWhen,
  ticketText,
  type HistoryEntry,
  type HistoryTicket,
} from '@/lib/history';
import { safeLink } from '@/lib/snapshot';
import { useNow } from '../CommandTable/useNow';
import styles from './History.module.css';

const KIND_CLASS = {
  moved: styles.ticket,
  left: styles.ticket,
  failed: styles.failed,
  queued: styles.queued,
} as const;

function TicketBadge({ ticket }: { ticket: HistoryTicket }) {
  const retry = useRetryTransition();
  const cls = `${styles.badge} ${KIND_CLASS[ticket.kind]}`;
  const link = safeLink(ticket.url, 'jira');
  const text = ticketText(ticket);
  return (
    <>
      {link ? (
        <a className={cls} href={link.href} target="_blank" rel="noopener noreferrer">
          {text}
        </a>
      ) : (
        <span className={cls}>{text}</span>
      )}
      {ticket.kind === 'failed' && ticket.retryId !== null && (
        <button
          type="button"
          className={styles.retry}
          aria-label={`Retry the move of ${ticket.key}`}
          disabled={retry.isPending}
          onClick={() => retry.mutate(ticket.retryId as number)}
        >
          Retry
        </button>
      )}
      {retry.isError && (
        <span className={styles.problem} role="alert">
          The retry could not be made.
        </span>
      )}
    </>
  );
}

function Row({ entry, now }: { entry: HistoryEntry; now: number }) {
  const url = historyUrl(entry);
  return (
    <li className={styles.row} data-pr={entry.prId}>
      <span className={styles.avatar} aria-hidden="true">
        {initials(entry.author)}
      </span>
      <div className={styles.main}>
        <h3 className={styles.prTitle}>
          {url ? (
            <a href={url} target="_blank" rel="noopener noreferrer">
              {entry.title}
            </a>
          ) : (
            entry.title
          )}
        </h3>
        <div className={styles.from}>
          {entry.repo} #{entry.number}
          {entry.author !== null && ` · ${entry.author}`}
        </div>
        {entry.problems.map((p) => (
          <div key={p} className={styles.reason}>
            {p}
          </div>
        ))}
      </div>
      {entry.tickets.length > 0 ? (
        <div className={styles.tickets}>
          {entry.tickets.map((t) => (
            <TicketBadge key={t.key} ticket={t} />
          ))}
        </div>
      ) : (
        <span className={`${styles.badge} ${styles.none}`}>No Jira link</span>
      )}
      {entry.otherFailed && (
        <span className={`${styles.badge} ${styles.failed}`}>Branch not deleted</span>
      )}
      {entry.additions !== null && entry.deletions !== null && (
        <span
          className={styles.diff}
          title={`${entry.additions} lines added, ${entry.deletions} removed`}
        >
          <span className={styles.add}>+{entry.additions}</span>{' '}
          <span className={styles.del}>
            {'−'}
            {entry.deletions}
          </span>
        </span>
      )}
      <span className={`${styles.badge} ${entry.queued ? styles.queued : styles.merged}`}>
        {entry.queued ? 'Queued' : 'Merged'} {mergedWhen(entry.at, now)}
      </span>
    </li>
  );
}

/**
 * The History view, which replaces the list: the pull requests merged from this dashboard since the
 * page was loaded, newest first, with what became of their Jira tickets. The header search filters
 * it. It reads the merges this tab made (the query cache), so it makes no call of its own.
 */
export function HistoryView({ terms }: { terms: readonly string[] }) {
  const merges = useRecentMerges().data;
  const now = useNow(60_000);
  const all = (merges ?? []).map((m) => historyEntry(m.pr, m.result, m.at));
  const list = all.filter((e) => matchesHistory(e, terms));
  return (
    <section className={styles.surface} aria-label="Merged pull requests">
      <div className={styles.head}>
        <h2 className={styles.title}>Merged pull requests</h2>
        <span className={styles.count} aria-label={`${list.length} merged`}>
          {list.length}
        </span>
      </div>
      {list.length > 0 ? (
        <ul className={styles.rows}>
          {list.map((e) => (
            <Row key={e.prId} entry={e} now={now} />
          ))}
        </ul>
      ) : (
        <p className={styles.empty}>
          {all.length === 0
            ? 'Nothing merged here yet. Pull requests you merge from this dashboard are listed until the page is reloaded.'
            : 'Nothing merged matches the search.'}
        </p>
      )}
    </section>
  );
}
