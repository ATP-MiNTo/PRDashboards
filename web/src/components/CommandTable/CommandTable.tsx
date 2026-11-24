'use client';

import { useState } from 'react';
import { useSnapshot, useSyncStatus } from '@/api/queries';
import type { Filter } from '@/lib/snapshot';
import { useTheme } from '@/theme/useTheme';
import styles from './CommandTable.module.css';
import { OrgBanner } from './OrgBanner';
import { Filters, StatStrip } from './StatStrip';
import { Table } from './Table';
import { TopBar } from './TopBar';
import { RecentMerges } from './RecentMerges';
import { SettingsDrawer } from '../Settings/SettingsDrawer';
import { useSnapshotEvents } from './useSnapshotEvents';

export function CommandTable() {
  const snapshot = useSnapshot();
  const status = useSyncStatus();
  const { theme, setTheme } = useTheme();
  const [filter, setFilter] = useState<Filter>('all');
  const [settingsOpen, setSettingsOpen] = useState(false);
  useSnapshotEvents();

  const data = snapshot.data;
  return (
    <div className={styles.page}>
      <TopBar
        snapshot={data}
        status={status.data}
        theme={theme}
        onTheme={setTheme}
        onSettings={() => setSettingsOpen(true)}
      />
      <main className={styles.wrap}>
        {snapshot.isPending && <p className={styles.muted}>Loading</p>}
        {snapshot.isError && <p className={styles.errorText}>Could not load the dashboard.</p>}
        {data && (
          <>
            <OrgBanner snapshot={data} />
            <StatStrip snapshot={data} />
            <Filters value={filter} onChange={setFilter} />
            {data.prs.length === 0 && data.sources.github.state === 'pending' ? (
              <p className={styles.muted}>Loading pull requests</p>
            ) : (
              <Table filter={filter} />
            )}
            <RecentMerges />
          </>
        )}
      </main>
      <SettingsDrawer open={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </div>
  );
}
