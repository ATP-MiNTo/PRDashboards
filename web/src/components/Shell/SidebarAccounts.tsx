'use client';

import { useAccounts, useSnapshot, useSyncStatus } from '@/api/queries';
import type { Accounts, Snapshot } from '@/api/client';
import styles from './Shell.module.css';

const METHOD = { app: 'GitHub App', oauth: 'OAuth', pat: 'Token' } as const;

type Tone = 'good' | 'warn';

function Badge({ tone, children }: { tone: Tone; children: string }) {
  return (
    <span className={`${styles.badge} ${tone === 'good' ? styles.good : styles.warn}`}>
      {children}
    </span>
  );
}

function GithubRow({ github }: { github: Accounts['github'] }) {
  const attention = github.needsReconnect === true || !!github.warning;
  return (
    <div className={styles.account}>
      <span>
        GitHub{' '}
        {github.connected && github.login && <span className={styles.dim}>{github.login}</span>}
      </span>
      <span className={styles.badges}>
        {!github.connected ? (
          <Badge tone="warn">Not connected</Badge>
        ) : (
          <>
            <Badge tone={attention ? 'warn' : 'good'}>{METHOD[github.method ?? 'app']}</Badge>
            {github.needsReconnect && <Badge tone="warn">Reconnect</Badge>}
          </>
        )}
      </span>
    </div>
  );
}

function JiraRow({ jira }: { jira: Accounts['jira'] }) {
  const site = jira.sites.find((s) => s.id === jira.selectedSiteId)?.name;
  return (
    <div className={styles.account}>
      <span>Jira {jira.connected && site && <span className={styles.dim}>{site}</span>}</span>
      <span className={styles.badges}>
        {jira.needsReconnect ? (
          <Badge tone="warn">Reconnect</Badge>
        ) : jira.connected ? (
          <Badge tone="good">Atlassian</Badge>
        ) : (
          <Badge tone="warn">Not connected</Badge>
        )}
      </span>
    </div>
  );
}

const rateLimitOf = (s: Snapshot) => s.sources.github.rateLimit;

/**
 * GitHub API usage ("API 4180/5000") from the last refresh, which the snapshot carries; the page
 * makes no call for it. Nothing is shown while the snapshot has no budget (before the first
 * refresh, when disconnected, or for a snapshot stored by an older version).
 */
function ApiUsage() {
  const fromSnapshot = useSnapshot(rateLimitOf).data;
  const status = useSyncStatus().data;
  // The status carries the budget of the latest refresh, also of the ones that left the snapshot as it was.
  const usage = status?.rateLimit ?? fromSnapshot;
  if (!usage) return null;
  return (
    <span className={styles.usage}>
      <span aria-hidden="true">
        API {usage.used}/{usage.limit}
      </span>
      <span className={styles.srOnly}>
        GitHub API usage {usage.used} of {usage.limit}
      </span>
    </span>
  );
}

/** The accounts block pinned at the bottom of the sidebar: who is connected, and the way to manage them. */
export function SidebarAccounts({ onManage }: { onManage: () => void }) {
  const accounts = useAccounts();
  return (
    <section className={styles.accounts} aria-labelledby="side-accounts">
      <div className={styles.accountsHead}>
        <span id="side-accounts" className={styles.eyebrow}>
          Accounts
        </span>
        <ApiUsage />
      </div>
      {accounts.isPending && <p className={styles.accountNote}>Loading accounts</p>}
      {accounts.isError && (
        <p className={styles.accountNote} role="alert">
          Could not load the accounts.
        </p>
      )}
      {accounts.data && (
        <>
          <GithubRow github={accounts.data.github} />
          <JiraRow jira={accounts.data.jira} />
        </>
      )}
      <button type="button" className={styles.manage} aria-haspopup="dialog" onClick={onManage}>
        Manage accounts
      </button>
    </section>
  );
}
