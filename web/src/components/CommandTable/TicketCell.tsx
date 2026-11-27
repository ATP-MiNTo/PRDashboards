import { Fragment, useState } from 'react';
import { ApiError, type SnapshotPr, type SnapshotTicket } from '@/api/client';
import { useAssignedTickets, useSaveTickets, useSyncNow } from '@/api/queries';
import { describeProblem } from '@/lib/problems';
import { waitingText } from '@/lib/rules';
import { Marquee } from '../Marquee/Marquee';
import { MenuItem, MenuNote, MenuTitle, PopMenu } from '../PopMenu/PopMenu';
import { useToast } from '../Toast/Toast';
import styles from './Controls.module.css';
import { PlusIcon } from './Icons';
import { TicketBadge } from './StackBanner';

const MAX_MANUAL = 20;
/** The search box of the menu shows only for a longer list. */
const SEARCH_MIN = 6;

const SOURCE: Record<SnapshotTicket['source'], string> = {
  branch: 'from branch',
  title: 'from title',
  manual: 'added manually',
};

/** What the badge says in place of a status when Jira did not answer for the ticket. */
function missing(lookup: SnapshotPr['ticketLookup']): string {
  return lookup === 'ok'
    ? 'Not found in Jira'
    : lookup === 'unverified'
      ? 'Not checked in Jira'
      : 'Not loaded';
}

/** The "My tickets" menu: the tickets assigned to the viewer, loaded only when it opens, and the linked ones to unlink. */
function MyTickets({
  pr,
  onLink,
  onUnlink,
  busy,
}: {
  pr: SnapshotPr;
  onLink: (key: string) => void;
  onUnlink: (t: SnapshotTicket) => void;
  busy: boolean;
}) {
  const [opened, setOpened] = useState(false);
  const [query, setQuery] = useState('');
  const assigned = useAssignedTickets(opened);
  const linked = new Set(pr.tickets.map((t) => t.key));
  const all = assigned.data?.tickets ?? [];
  const q = query.trim().toLowerCase();
  const options = all.filter(
    (t) =>
      !linked.has(t.key) &&
      (q === '' || t.key.toLowerCase().includes(q) || t.summary.toLowerCase().includes(q)),
  );
  const manual = pr.tickets.filter((t) => t.source === 'manual').length;
  const full = manual >= MAX_MANUAL;
  const error = assigned.error;

  return (
    <PopMenu
      label="Link one of my tickets"
      menuLabel="My tickets"
      triggerClassName={styles.add}
      trigger={<PlusIcon />}
      width="w72"
      side="auto"
      disabled={busy}
      onOpen={() => {
        setOpened(true);
        setQuery('');
      }}
    >
      {(close) => (
        <>
          <MenuTitle>My tickets</MenuTitle>
          {assigned.isSuccess && all.length >= SEARCH_MIN && (
            <li role="none">
              <input
                type="search"
                className={styles.search}
                aria-label="Search your tickets"
                placeholder="Search your tickets"
                autoComplete="off"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
            </li>
          )}
          {assigned.isPending && <MenuNote>Loading tickets</MenuNote>}
          {assigned.isError && (
            <MenuNote>
              {error instanceof ApiError && error.code === 'not_connected'
                ? 'Jira is not connected.'
                : 'Could not load your tickets.'}
            </MenuNote>
          )}
          {assigned.isSuccess &&
            options.map((t) => (
              <MenuItem
                key={t.key}
                disabled={full}
                onSelect={() => {
                  close();
                  onLink(t.key);
                }}
              >
                <span className={styles.itemKey}>{t.key}</span>
                <span className={styles.itemSummary}>{t.summary}</span>
                <span className={styles.itemBadge}>{t.status}</span>
              </MenuItem>
            ))}
          {assigned.isSuccess && options.length === 0 && (
            <MenuNote>
              {q === '' ? 'No more tickets assigned to you' : 'No matching tickets'}
            </MenuNote>
          )}
          {full && <MenuNote>Twenty manual links is the most for one pull request</MenuNote>}
          {pr.tickets.length > 0 && <MenuTitle>On this pull request</MenuTitle>}
          {pr.tickets.map((t) => (
            <MenuItem
              key={t.key}
              onSelect={() => {
                close();
                onUnlink(t);
              }}
            >
              <span className={styles.itemSummary}>Unlink {t.key}</span>
              <span className={styles.itemBadge}>{SOURCE[t.source]}</span>
            </MenuItem>
          ))}
        </>
      )}
    </PopMenu>
  );
}

/**
 * The Jira cell: one outlined badge per linked ticket ("KEY · Status", a link to Jira), or "No Jira
 * link", and the round plus button that opens the "My tickets" menu to link another ticket or
 * unlink one. The tickets assigned to the viewer are read from Jira only when the menu opens.
 */
export function TicketCell({ pr }: { pr: SnapshotPr }) {
  const save = useSaveTickets();
  const sync = useSyncNow();
  const toast = useToast();

  const manual = pr.tickets.filter((t) => t.source === 'manual').map((t) => t.key);
  const apply = (nextManual: string[], nextRemoved: string[]) =>
    save.mutate(
      { id: pr.id, links: { manual: nextManual, removed: nextRemoved } },
      {
        onError: (e) =>
          toast.show(
            e instanceof ApiError
              ? describeProblem(e.code, e.detail)
              : 'Could not save the change.',
            'error',
          ),
      },
    );
  const unlink = (t: SnapshotTicket) =>
    t.source === 'manual'
      ? apply(
          manual.filter((k) => k !== t.key),
          pr.removedKeys,
        )
      : apply(manual, [...pr.removedKeys, t.key]);
  const link = (key: string) =>
    apply(
      [...manual, key],
      pr.removedKeys.filter((k) => k !== key),
    );

  const waiting = pr.tickets.filter((t) => t.waiting);
  return (
    <div className={styles.tkRoot}>
      <span className={styles.tickets}>
        <Marquee className={styles.ticketsMq}>
          {pr.tickets.length === 0 ? (
            <span className={styles.noJira}>No Jira link</span>
          ) : (
            pr.tickets.map((t, i) => (
              <Fragment key={t.key}>
                {i > 0 && ' '}
                <TicketBadge
                  ticketKey={t.key}
                  status={t.found ? t.status : missing(pr.ticketLookup)}
                  url={t.url}
                  title={
                    t.found && t.summary ? `${t.summary}, ${SOURCE[t.source]}` : SOURCE[t.source]
                  }
                />
              </Fragment>
            ))
          )}
        </Marquee>
        <MyTickets pr={pr} onLink={link} onUnlink={unlink} busy={save.isPending} />
      </span>
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
      {waiting.map(
        (t) =>
          t.waiting && (
            <p key={t.key} className={`${styles.tkNote} ${styles.tkWarn}`} role="status">
              {t.key}: {waitingText(t.waiting)}
            </p>
          ),
      )}
    </div>
  );
}
