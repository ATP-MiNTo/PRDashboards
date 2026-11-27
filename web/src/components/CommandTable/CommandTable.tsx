'use client';

import { useMemo, useRef, useState } from 'react';
import type { Snapshot } from '@/api/client';
import { useSnapshot, useSyncStatus } from '@/api/queries';
import { showCounts } from '@/lib/show';
import { useTheme } from '@/theme/useTheme';
import styles from './CommandTable.module.css';
import { OrgBanner } from './OrgBanner';
import { StatStrip } from './StatStrip';
import { List, Notice } from './List';
import { RecentMerges } from './RecentMerges';
import { AccountsPanel } from '../AccountsPanel';
import { Footer } from '../Toolbar/Footer';
import { Toolbar } from '../Toolbar/Toolbar';
import { useListPrefs } from '../Toolbar/useListPrefs';
import { SidePanel } from '../SidePanel/SidePanel';
import { RulesModal } from '../RulesModal/RulesModal';
import { SettingsContent } from '../Settings/SettingsContent';
import { Header } from '../Header/Header';
import { SearchBox } from '../Header/SearchBox';
import { Sidebar } from '../Shell/Sidebar';
import { SidebarAccounts } from '../Shell/SidebarAccounts';
import { useScrolled, useSidebar } from '../Shell/useSidebar';
import { ToastProvider } from '../Toast/Toast';
import { useSnapshotEvents } from './useSnapshotEvents';

const NO_IDS: string[] = [];
const selectIds = (s: Snapshot) => s.prs.map((p) => p.id);
const NO_SHOW_COUNTS = { everything: 0, assigned: 0, authored: 0 };

export function CommandTable() {
  const snapshot = useSnapshot();
  const status = useSyncStatus();
  const theme = useTheme(); // keeps the mode on <html> in step with the setting
  const ids = useSnapshot(selectIds).data ?? NO_IDS;
  const shown = useSnapshot(showCounts).data ?? NO_SHOW_COUNTS;
  const prefs = useListPrefs(ids);
  const [account, setAccount] = useState(false);
  const [rules, setRules] = useState(false);
  const sidebar = useSidebar({ hold: account || rules });
  const scrolled = useScrolled();
  const burger = useRef<HTMLButtonElement>(null);
  useSnapshotEvents();
  const { filter, show, terms, hidden, mode } = prefs;
  const view = useMemo(
    () => ({ filter, show, terms, hidden, mode }),
    [filter, show, terms, hidden, mode],
  );

  const data = snapshot.data;
  return (
    <ToastProvider>
      <div className={styles.page}>
        <Sidebar
          id="settings-sidebar"
          open={sidebar.open}
          overlay={!sidebar.wide}
          onClose={sidebar.close}
          asideRef={sidebar.aside}
          returnFocus={burger}
          footer={<SidebarAccounts onManage={() => setAccount(true)} />}
        >
          <SettingsContent onOpenRules={() => setRules(true)} />
        </Sidebar>
        <div className={styles.main}>
          <Header
            stuck={scrolled}
            sidebarOpen={sidebar.open}
            sidebarId="settings-sidebar"
            onToggleSidebar={sidebar.toggle}
            burgerRef={burger}
            query={prefs.query}
            onQuery={prefs.setQuery}
            snapshot={data}
            status={status.data}
            theme={theme.theme}
            mode={theme.mode}
            onTheme={theme.setTheme}
          />
          <main className={styles.wrap}>
            <SearchBox value={prefs.query} onChange={prefs.setQuery} variant="page" />
            {snapshot.isPending && <Notice>Loading</Notice>}
            {snapshot.isError && <Notice error>Could not load the dashboard.</Notice>}
            {data && (
              <>
                <OrgBanner snapshot={data} />
                <StatStrip view={view} onFilter={prefs.setFilter} />
                <Toolbar
                  sort={prefs.sort}
                  reversed={prefs.reversed}
                  onSort={prefs.setSort}
                  onReverse={prefs.reverse}
                  show={prefs.show}
                  onShow={prefs.setShow}
                  showCounts={shown}
                  mode={prefs.mode}
                  onMode={prefs.setMode}
                  counts={{
                    visible: Math.max(0, ids.length - prefs.hidden.size),
                    all: ids.length,
                    hidden: prefs.hidden.size,
                  }}
                />
                {data.prs.length === 0 && data.sources.github.state === 'pending' ? (
                  <Notice>Loading pull requests</Notice>
                ) : (
                  <List prefs={prefs} />
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
        {rules && <RulesModal onClose={() => setRules(false)} />}
      </div>
    </ToastProvider>
  );
}
