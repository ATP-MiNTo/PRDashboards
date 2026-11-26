'use client';

import { useRef, useState } from 'react';
import type { Snapshot } from '@/api/client';
import { useSnapshot, useSyncStatus } from '@/api/queries';
import { useTheme } from '@/theme/useTheme';
import styles from './CommandTable.module.css';
import { OrgBanner } from './OrgBanner';
import { Filters, StatStrip } from './StatStrip';
import { Table } from './Table';
import { TopBar } from './TopBar';
import { RecentMerges } from './RecentMerges';
import { AccountsPanel } from '../AccountsPanel';
import { Footer } from '../Toolbar/Footer';
import { Toolbar } from '../Toolbar/Toolbar';
import { useListPrefs } from '../Toolbar/useListPrefs';
import { SidePanel } from '../SidePanel/SidePanel';
import { SettingsContent } from '../Settings/SettingsContent';
import { Burger } from '../Shell/Burger';
import { Sidebar } from '../Shell/Sidebar';
import { useScrolled, useSidebar } from '../Shell/useSidebar';
import { useSnapshotEvents } from './useSnapshotEvents';

const NO_IDS: string[] = [];
const selectIds = (s: Snapshot) => s.prs.map((p) => p.id);

export function CommandTable() {
  const snapshot = useSnapshot();
  const status = useSyncStatus();
  useTheme(); // keeps the mode on <html> in step with the setting
  const ids = useSnapshot(selectIds).data ?? NO_IDS;
  const prefs = useListPrefs(ids);
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
              <div className={styles.controls}>
                <Filters value={prefs.filter} onChange={prefs.setFilter} />
                <Toolbar
                  sort={prefs.sort}
                  reversed={prefs.reversed}
                  onSort={prefs.setSort}
                  onReverse={prefs.reverse}
                  mode={prefs.mode}
                  onMode={prefs.setMode}
                  counts={{
                    visible: Math.max(0, ids.length - prefs.hidden.size),
                    all: ids.length,
                    hidden: prefs.hidden.size,
                  }}
                />
              </div>
              {data.prs.length === 0 && data.sources.github.state === 'pending' ? (
                <p className={styles.muted}>Loading pull requests</p>
              ) : (
                <Table prefs={prefs} />
              )}
              <RecentMerges />
            </>
          )}
        </main>
        <Footer />
      </div>
      <SidePanel open={account} onClose={() => setAccount(false)} title="Account">
        <AccountsPanel />
      </SidePanel>
    </div>
  );
}
