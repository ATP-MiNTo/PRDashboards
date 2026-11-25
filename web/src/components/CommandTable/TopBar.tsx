import Link from 'next/link';
import type { Snapshot, SourceStatus, SyncStatus } from '@/api/client';
import { useSyncNow } from '@/api/queries';
import { countdown, sourceLabel } from '@/lib/snapshot';
import type { ThemeSetting } from '@/theme/theme';
import styles from './CommandTable.module.css';
import { RelativeTime } from './RelativeTime';
import { useNow } from './useNow';

const THEMES: { value: ThemeSetting; label: string }[] = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'auto', label: 'Auto' },
];

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
  theme,
  onTheme,
  onSettings,
}: {
  snapshot: Snapshot | undefined;
  status: SyncStatus | null | undefined;
  theme: ThemeSetting | undefined;
  onTheme: (t: ThemeSetting) => void;
  onSettings: () => void;
}) {
  const sync = useSyncNow();
  const running = status?.running === true || sync.isPending;
  const githubAt = status?.githubAt ?? snapshot?.sources.github.syncedAt ?? null;
  const jiraAt = status?.jiraAt ?? snapshot?.sources.jira.syncedAt ?? null;
  return (
    <header className={styles.topbar}>
      <div className={styles.topbarIn}>
        <div className={styles.brand}>
          <b>PR Dashboards</b>
          <span>assigned or opened by you</span>
        </div>
        <div className={styles.tbRight}>
          <div className={styles.seg} role="group" aria-label="Theme">
            {THEMES.map((t) => (
              <button
                key={t.value}
                type="button"
                aria-pressed={theme === t.value}
                onClick={() => onTheme(t.value)}
              >
                {t.label}
              </button>
            ))}
          </div>
          <button
            type="button"
            className={running ? `${styles.tbButton} ${styles.spin}` : styles.tbButton}
            disabled={running}
            onClick={() => sync.mutate()}
          >
            {running ? 'Syncing' : 'Sync'}
          </button>
          <Link className={styles.tbButton} href="/accounts/">
            Accounts
          </Link>
          <button
            type="button"
            className={styles.tbButton}
            aria-haspopup="dialog"
            onClick={onSettings}
          >
            Settings
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
