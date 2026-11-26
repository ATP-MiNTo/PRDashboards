'use client';

import { useRef, useState } from 'react';
import { useSnapshot, useSyncStatus } from '@/api/queries';
import type { Filter } from '@/lib/snapshot';
import { useTheme } from '@/theme/useTheme';
import styles from './CommandTable.module.css';
import { OrgBanner } from './OrgBanner';
import { Filters, StatStrip } from './StatStrip';
import { Table } from './Table';
import { TopBar } from './TopBar';
import { RecentMerges } from './RecentMerges';
import { AccountsPanel } from '../AccountsPanel';
import { SidePanel } from '../SidePanel/SidePanel';
import { SettingsContent } from '../Settings/SettingsContent';
import { Burger } from '../Shell/Burger';
import { Sidebar } from '../Shell/Sidebar';
import { useScrolled, useSidebar } from '../Shell/useSidebar';
import { useSnapshotEvents } from './useSnapshotEvents';

export function CommandTable() {
  const snapshot = useSnapshot();
  const status = useSyncStatus();
  useTheme(); // keeps the mode on <html> in step with the setting
  const [filter, setFilter] = useState<Filter>('all');
  const [account, setAccount] = useState(false);
  const sidebar = useSidebar({ hold: account });
  const scrolled = useScrolled();
  const burger = useRef<HTMLButtonElement>(null);
  useSnapshotEvents();

  const data = snapshot.data;
  return (
    <div className={styles.page}>
      <Sidebar
        id="settings-sidebar"
        open={sidebar.open}
        overlay={!sidebar.wide}
        onClose={sidebar.close}
        asideRef={sidebar.aside}
        returnFocus={burger}
      >
        <SettingsContent />
      </Sidebar>
      <div className={styles.main}>
        <Burger
          open={sidebar.open}
          stuck={scrolled}
          onToggle={sidebar.toggle}
          buttonRef={burger}
          controls="settings-sidebar"
        />
        <TopBar
          snapshot={data}
          status={status.data}
          onAccount={() => setAccount(true)}
          scrolled={scrolled}
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
      </div>
      <SidePanel open={account} onClose={() => setAccount(false)} title="Account">
        <AccountsPanel />
      </SidePanel>
    </div>
  );
}
