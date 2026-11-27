'use client';

import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { flushSync } from 'react-dom';
import type { Snapshot } from '@/api/client';
import { useSnapshot, useSyncStatus } from '@/api/queries';
import { showCounts } from '@/lib/show';
import { crossfade } from '@/theme/transition';
import { reducedMotion } from '@/lib/storage';
import { useTheme } from '@/theme/useTheme';
import styles from './CommandTable.module.css';
import { OrgBanner } from './OrgBanner';
import { CompactStats, StatStrip, useListStats } from './StatStrip';
import { List, Notice } from './List';
import { AccountsModal } from '../Accounts/AccountsModal';
import { HistoryView } from '../History/HistoryView';
import { Footer } from '../Toolbar/Footer';
import { Toolbar } from '../Toolbar/Toolbar';
import { useListPrefs } from '../Toolbar/useListPrefs';
import { RulesModal } from '../RulesModal/RulesModal';
import { SettingsContent } from '../Settings/SettingsContent';
import { Header } from '../Header/Header';
import { SearchBox } from '../Header/SearchBox';
import { Sidebar } from '../Shell/Sidebar';
import { SidebarAccounts } from '../Shell/SidebarAccounts';
import { useScrolled, useSidebar } from '../Shell/useSidebar';
import { ToastProvider } from '../Toast/Toast';
import { useNow } from './useNow';
import { useSnapshotEvents } from './useSnapshotEvents';
import { useStripOut } from './useStripOut';

const NO_IDS: string[] = [];
const selectIds = (s: Snapshot) => s.prs.map((p) => p.id);
const HISTORY_TITLE = 'PRDashboards · History';
const NO_SHOW_COUNTS = { all: 0, mine: 0, review: 0 };

export function CommandTable() {
  const snapshot = useSnapshot();
  const status = useSyncStatus();
  const theme = useTheme(); // keeps the mode on <html> in step with the setting
  const ids = useSnapshot(selectIds).data ?? NO_IDS;
  const prefs = useListPrefs(ids);
  const { filter, show, terms, hidden, mode, off } = prefs;
  const shown = useSnapshot((s: Snapshot) => showCounts(s, off)).data ?? NO_SHOW_COUNTS;
  const listed =
    useSnapshot((s: Snapshot) => s.prs.filter((p) => !off.has(p.repo.id)).map((p) => p.id)).data ??
    NO_IDS;
  const [account, setAccount] = useState(false);
  const [rules, setRules] = useState(false);
  const [history, setHistory] = useState(false);
  const sidebar = useSidebar();
  const scrolled = useScrolled();
  const burger = useRef<HTMLButtonElement>(null);
  useSnapshotEvents();
  // The clock of the Stale filter: a minute is fine for a three day limit.
  const now = useNow(60_000);
  const view = useMemo(
    () => ({ filter, show, terms, hidden, mode, off, now }),
    [filter, show, terms, hidden, mode, off, now],
  );
  const stats = useListStats(view);
  // Once the strip has scrolled away, the same tiles show in the right header pill.
  const strip = useStripOut();
  // The History view replaces the list with a cross-fade of the whole page (0.9s).
  const toggleHistory = () =>
    crossfade(document, () => flushSync(() => setHistory((h) => !h)), reducedMotion());

  // The History view re-tints the whole page, light and dark, through a `data-view` on <html>
  // (the warm palette of the design, in globals.css). Set in the same commit as the view, so the
  // cross-fade of the toggle carries it.
  useLayoutEffect(() => {
    if (!history) return;
    const root = document.documentElement;
    root.dataset.view = 'history';
    return () => {
      delete root.dataset.view;
    };
  }, [history]);

  const hiddenListed = listed.filter((id) => hidden.has(id)).length;
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
          <SettingsContent onOpenRules={() => setRules(true)} off={off} onOff={prefs.setOff} />
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
            history={{ active: history, onToggle: toggleHistory }}
            title={history ? HISTORY_TITLE : undefined}
            compactStats={
              stats && !history ? (
                <CompactStats stats={stats} filter={filter} onFilter={prefs.setFilter} />
              ) : undefined
            }
            showCompactStats={strip.out && !history}
          />
          <main className={styles.wrap}>
            <SearchBox value={prefs.query} onChange={prefs.setQuery} variant="page" />
            {snapshot.isPending && <Notice>Loading</Notice>}
            {snapshot.isError && <Notice error>Could not load the dashboard.</Notice>}
            {data && (
              <>
                <OrgBanner snapshot={data} />
                {history ? (
                  <HistoryView terms={terms} />
                ) : (
                  <>
                    {stats && (
                      <StatStrip
                        stats={stats}
                        filter={filter}
                        onFilter={prefs.setFilter}
                        stripRef={strip.ref}
                      />
                    )}
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
                        visible: Math.max(0, listed.length - hiddenListed),
                        all: listed.length,
                        hidden: hiddenListed,
                      }}
                    />
                    {data.prs.length === 0 && data.sources.github.state === 'pending' ? (
                      <Notice>Loading pull requests</Notice>
                    ) : (
                      <List prefs={prefs} now={now} />
                    )}
                  </>
                )}
              </>
            )}
          </main>
          <Footer />
        </div>
        {account && <AccountsModal onClose={() => setAccount(false)} />}
        {rules && <RulesModal onClose={() => setRules(false)} />}
      </div>
    </ToastProvider>
  );
}
