import type { Snapshot, SourceStatus, SyncStatus } from '@/api/client';
import { useSyncNow } from '@/api/queries';
import { countdown, sourceLabel } from '@/lib/snapshot';
import { BrandIcon } from '../Shell/BrandIcon';
import styles from './CommandTable.module.css';
import { RelativeTime } from './RelativeTime';
import { useNow } from './useNow';

function Countdown({ iso }: { iso: string | null }) {
  const now = useNow(1000);
  const text = countdown(iso, now);
  return text ? <span>Next sync in {text}</span> : null;
}

function Source({ name, source }: { name: string; source: SourceStatus }) {
  const now = useNow(1000);
  const label = sourceLabel(source, now);
  return (
    <span className={styles.sourceItem} data-state={source.state}>
      {name} <b>{label}</b>
    </span>
  );
}

export function TopBar({
  snapshot,
  status,
  onAccount,
  scrolled = false,
}: {
  snapshot: Snapshot | undefined;
  status: SyncStatus | null | undefined;
  onAccount: () => void;
  /** True once the page has scrolled: the floating pill shows the name instead. */
  scrolled?: boolean;
}) {
  const sync = useSyncNow();
  const running = status?.running === true || sync.isPending;
  const githubAt = status?.githubAt ?? snapshot?.sources.github.syncedAt ?? null;
  const jiraAt = status?.jiraAt ?? snapshot?.sources.jira.syncedAt ?? null;
  return (
    <header className={styles.topbar}>
      <div className={styles.topbarIn}>
        <div className={scrolled ? `${styles.brand} ${styles.brandAway}` : styles.brand}>
          <b>PR Dashboards</b>
          <BrandIcon className={styles.brandIcon} />
          <span>assigned or opened by you</span>
        </div>
        <div className={styles.tbRight}>
          <button
            type="button"
            className={running ? `${styles.tbButton} ${styles.spin}` : styles.tbButton}
            disabled={running}
            onClick={() => sync.mutate()}
          >
            {running ? 'Syncing' : 'Sync'}
          </button>
          <button
            type="button"
            className={styles.tbButton}
            aria-haspopup="dialog"
            onClick={onAccount}
          >
            Account
          </button>
        </div>
        <div className={styles.tbNote}>
          {snapshot && (
            <span className={styles.sources} role="status" aria-label="Source status">
              <Source name="GitHub" source={snapshot.sources.github} />
              <Source name="Jira" source={snapshot.sources.jira} />
            </span>
          )}
          <span>
            GitHub synced <RelativeTime iso={githubAt} />
          </span>
          <span>
            Jira synced <RelativeTime iso={jiraAt} />
          </span>
          <Countdown iso={status?.nextGithubAt ?? null} />
        </div>
      </div>
    </header>
  );
}
