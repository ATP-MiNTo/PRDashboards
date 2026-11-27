import type { Snapshot, SourceStatus, SyncStatus } from '@/api/client';
import { countdown, sourceLabel } from '@/lib/snapshot';
import { RelativeTime } from '../CommandTable/RelativeTime';
import { useNow } from '../CommandTable/useNow';
import styles from './Header.module.css';

function Countdown({ iso }: { iso: string | null }) {
  const now = useNow(1000);
  const text = countdown(iso, now);
  return text ? <span>Next sync in {text}</span> : null;
}

function Source({ name, source }: { name: string; source: SourceStatus }) {
  const now = useNow(1000);
  const label = sourceLabel(source, now);
  return (
    <span className={styles.source} data-state={source.state}>
      {name} <b>{label}</b>
    </span>
  );
}

/** The state of both sources and the sync times: the content of the refresh button's popover. */
export function SyncDetails({
  snapshot,
  status,
}: {
  snapshot: Snapshot | undefined;
  status: SyncStatus | null | undefined;
}) {
  const githubAt = status?.githubAt ?? snapshot?.sources.github.syncedAt ?? null;
  const jiraAt = status?.jiraAt ?? snapshot?.sources.jira.syncedAt ?? null;
  return (
    <>
      <b className={styles.popTitle}>Sync status</b>
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
    </>
  );
}
