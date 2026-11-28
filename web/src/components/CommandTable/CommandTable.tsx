'use client';

import {
  lazy,
  memo,
  Suspense,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from 'react';
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
import { Footer } from '../Toolbar/Footer';
import { Toolbar } from '../Toolbar/Toolbar';
import { useListPrefs } from '../Toolbar/useListPrefs';
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
/** How often the clock of the Stale filter ticks. */
export const STALE_TICK_MS = 3_600_000;
/** How long after the page is drawn the code of the History view is fetched. */
const HISTORY_PRELOAD_MS = 1500;
const NO_SHOW_COUNTS = { all: 0, mine: 0, review: 0 };

// The dialogs and the History view load when they are first opened, not with the page.
const loadAccounts = () => import('../Accounts/AccountsModal');
const loadRules = () => import('../RulesModal/RulesModal');
const loadHistory = () => import('../History/HistoryView');
const AccountsModal = lazy(() => loadAccounts().then((m) => ({ default: m.AccountsModal })));
const RulesModal = lazy(() => loadRules().then((m) => ({ default: m.RulesModal })));
const HistoryView = lazy(() => loadHistory().then((m) => ({ default: m.HistoryView })));

/** The Settings sidebar. It renders again only when what it shows changes, not for a keystroke in the search. */
const SidebarPane = memo(function SidebarPane({
  open,
  overlay,
  onClose,
  asideRef,
  returnFocus,
  onManage,
  onOpenRules,
  off,
  onOff,
}: {
  open: boolean;
  overlay: boolean;
  onClose: () => void;
  asideRef: RefObject<HTMLElement | null>;
  returnFocus: RefObject<HTMLElement | null>;
  onManage: () => void;
  onOpenRules: () => void;
  off: ReadonlySet<string>;
  onOff: (next: ReadonlySet<string>) => void;
}) {
  return (
    <Sidebar
      id="settings-sidebar"
      open={open}
      overlay={overlay}
      onClose={onClose}
      asideRef={asideRef}
      returnFocus={returnFocus}
      footer={<SidebarAccounts onManage={onManage} />}
    >
      <SettingsContent onOpenRules={onOpenRules} off={off} onOff={onOff} />
    </Sidebar>
  );
});

export function CommandTable() {
  const snapshot = useSnapshot();
  const status = useSyncStatus();
  const theme = useTheme(); // keeps the mode on <html> in step with the setting
  const ids = useSnapshot(selectIds).data ?? NO_IDS;
  const prefs = useListPrefs(ids);
  const { filter, show, terms, hidden, mode, off, setFilter, setOff } = prefs;
  const selectShown = useCallback((s: Snapshot) => showCounts(s, off), [off]);
  const selectListed = useCallback(
    (s: Snapshot) => s.prs.filter((p) => !off.has(p.repo.id)).map((p) => p.id),
    [off],
  );
  const shown = useSnapshot(selectShown).data ?? NO_SHOW_COUNTS;
  const listed = useSnapshot(selectListed).data ?? NO_IDS;
  const hiddenListed = useMemo(
    () => listed.filter((id) => hidden.has(id)).length,
    [listed, hidden],
  );
  const [account, setAccount] = useState(false);
  const [rules, setRules] = useState(false);
  const [history, setHistory] = useState(false);
  const sidebar = useSidebar();
  const scrolled = useScrolled();
  const burger = useRef<HTMLButtonElement>(null);
  useSnapshotEvents();
  // The clock of the Stale filter, which has a three day limit: an hour is fine, and the list does
  // not render again every minute. Times shown on a card tick on their own.
  const now = useNow(STALE_TICK_MS);
  const view = useMemo(
    () => ({ filter, show, terms, hidden, mode, off, now }),
    [filter, show, terms, hidden, mode, off, now],
  );
  const stats = useListStats(view);
  // Once the strip has scrolled away, the same tiles show in the right header pill.
  const strip = useStripOut();
  // The History view replaces the list with a cross-fade of the whole page (0.9s).
  const toggleHistory = useCallback(
    () => crossfade(document, () => flushSync(() => setHistory((h) => !h)), reducedMotion()),
    [],
  );
  // Its code loads once the page is idle, so the first toggle fades into a finished view.
  useEffect(() => {
    const id = setTimeout(() => void loadHistory(), HISTORY_PRELOAD_MS);
    return () => clearTimeout(id);
  }, []);
  const historyProp = useMemo(
    () => ({ active: history, onToggle: toggleHistory }),
    [history, toggleHistory],
  );
  const openAccounts = useCallback(() => setAccount(true), []);
  const closeAccounts = useCallback(() => setAccount(false), []);
  const openRules = useCallback(() => setRules(true), []);
  const closeRules = useCallback(() => setRules(false), []);
  const compactStats = useMemo(
    () =>
      stats && !history ? (
        <CompactStats stats={stats} filter={filter} onFilter={setFilter} />
      ) : undefined,
    [stats, history, filter, setFilter],
  );
  const counts = useMemo(
    () => ({
      visible: Math.max(0, listed.length - hiddenListed),
      all: listed.length,
      hidden: hiddenListed,
    }),
    [listed, hiddenListed],
  );

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

  const data = snapshot.data;
  return (
    <ToastProvider>
      <div className={styles.page}>
        <SidebarPane
          open={sidebar.open}
          overlay={!sidebar.wide}
          onClose={sidebar.close}
          asideRef={sidebar.aside}
          returnFocus={burger}
          onManage={openAccounts}
          onOpenRules={openRules}
          off={off}
          onOff={setOff}
        />
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
            history={historyProp}
            title={history ? HISTORY_TITLE : undefined}
            compactStats={compactStats}
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
                  <Suspense fallback={null}>
                    <HistoryView terms={terms} />
                  </Suspense>
                ) : (
                  <>
                    {stats && (
                      <StatStrip
                        stats={stats}
                        filter={filter}
                        onFilter={setFilter}
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
                      counts={counts}
                    />
                    {data.prs.length === 0 && data.sources.github.state === 'pending' ? (
                      <Notice>Loading pull requests</Notice>
                    ) : (
                      <List prefs={prefs} now={now} onConnect={openAccounts} />
                    )}
                  </>
                )}
              </>
            )}
          </main>
          <Footer />
        </div>
        <Suspense fallback={null}>
          {account && <AccountsModal onClose={closeAccounts} />}
          {rules && <RulesModal onClose={closeRules} />}
        </Suspense>
      </div>
    </ToastProvider>
  );
}
