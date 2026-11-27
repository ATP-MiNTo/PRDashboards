'use client';

import { useAccounts } from '@/api/queries';
import type { Accounts } from '@/api/client';
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

/** The accounts block pinned at the bottom of the sidebar: who is connected, and the way to manage them. */
export function SidebarAccounts({ onManage }: { onManage: () => void }) {
  const accounts = useAccounts();
  return (
    <section className={styles.accounts} aria-labelledby="side-accounts">
      <div className={styles.accountsHead}>
        <span id="side-accounts" className={styles.eyebrow}>
          Accounts
        </span>
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
