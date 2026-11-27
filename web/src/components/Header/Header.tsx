'use client';

import type { ReactNode, RefObject } from 'react';
import type { Snapshot, SyncStatus } from '@/api/client';
import type { Mode, ThemeSetting } from '@/theme/theme';
import { BrandIcon } from '../Shell/BrandIcon';
import styles from './Header.module.css';
import { HistoryButton } from './HistoryButton';
import { Icon } from './Icon';
import { RefreshButton } from './RefreshButton';
import { SearchBox } from './SearchBox';
import { ThemeButton } from './ThemeButton';

export interface HeaderProps {
  /** True once the page has scrolled: the pills turn to glass and the left one shows the name. */
  stuck: boolean;
  sidebarOpen: boolean;
  sidebarId: string;
  onToggleSidebar: () => void;
  burgerRef: RefObject<HTMLButtonElement | null>;
  query: string;
  onQuery: (q: string) => void;
  snapshot: Snapshot | undefined;
  status: SyncStatus | null | undefined;
  theme: ThemeSetting | undefined;
  mode: Mode | undefined;
  onTheme: (t: ThemeSetting) => void;
  /** The history view toggle. Without it the header has no history button. */
  history?: { active: boolean; onToggle: () => void };
  /** The page title in the title bar. */
  title?: ReactNode;
  /** The summary shown in the right pill once the stat strip has scrolled away, from 1024px up. */
  compactStats?: ReactNode;
  showCompactStats?: boolean;
}

const NAME = 'PRDashboards';

/**
 * The header: two floating pills that stay at the top while the page scrolls (burger and name on
 * the left, search and the refresh, history and theme circles on the right), over a glass title
 * bar that scrolls away. The pills are transparent until the page scrolls.
 */
export function Header({
  stuck,
  sidebarOpen,
  sidebarId,
  onToggleSidebar,
  burgerRef,
  query,
  onQuery,
  snapshot,
  status,
  theme,
  mode,
  onTheme,
  history,
  title = NAME,
  compactStats,
  showCompactStats = false,
}: HeaderProps) {
  const pill = (side?: string) => `${styles.pill} ${side}${stuck ? ` ${styles.stuck}` : ''}`;
  return (
    <>
      <div className={styles.bar}>
        <div className={pill(styles.pillLeft)} data-stuck={stuck}>
          <button
            ref={burgerRef}
            type="button"
            className={styles.burger}
            aria-label="Settings sidebar"
            aria-expanded={sidebarOpen}
            aria-controls={sidebarId}
            onClick={onToggleSidebar}
          >
            <Icon>
              <path d="M4 6h16M4 12h16M4 18h16" />
            </Icon>
          </button>
          <span className={styles.pillTitle} aria-hidden="true">
            <span>{NAME}</span>
            <BrandIcon />
          </span>
        </div>
        <div className={pill(styles.pillRight)}>
          <div
            className={showCompactStats ? `${styles.stats} ${styles.statsOn}` : styles.stats}
            role={compactStats ? 'group' : undefined}
            aria-label={compactStats ? 'Summary' : undefined}
            aria-hidden={compactStats ? undefined : true}
          >
            {compactStats}
          </div>
          <SearchBox value={query} onChange={onQuery} variant="bar" />
          <RefreshButton snapshot={snapshot} status={status} />
          {history && <HistoryButton active={history.active} onToggle={history.onToggle} />}
          <ThemeButton theme={theme} mode={mode} onTheme={onTheme} />
        </div>
      </div>
      <div className={styles.titleWrap}>
        <div className={styles.titleBar}>
          <h1 className={styles.title}>
            <span>{title}</span>
            <BrandIcon />
          </h1>
        </div>
      </div>
    </>
  );
}
