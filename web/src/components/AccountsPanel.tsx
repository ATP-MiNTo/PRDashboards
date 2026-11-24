'use client';

import { useState } from 'react';
import styles from '@/app/page.module.css';
import { ApiError } from '@/api/client';
import {
  useAccounts,
  useCheck,
  useDisconnect,
  useRevokeAll,
  useSaveSettings,
  useSettings,
  useSetup,
} from '@/api/queries';
import type { Provider } from '@/api/client';
import { describeProblem } from '@/lib/problems';

function problemText(error: unknown): string {
  return describeProblem(error instanceof ApiError ? error.code : undefined);
}

interface ConnectProps {
  provider: Provider;
  label: string;
  configured: boolean;
  primary?: boolean;
}

function Connect({ provider, label, configured, primary }: ConnectProps) {
  const cls = `${styles.link} ${primary ? styles.primary : ''}`;
  return configured ? (
    <a className={cls} href={`/oauth/${provider}/start`}>
      {label}
    </a>
  ) : (
    <a className={styles.link} href="/setup/">
      Set up the OAuth app
    </a>
  );
}

export function AccountsPanel() {
  const accounts = useAccounts();
  const setup = useSetup();
  const settings = useSettings();
  const disconnect = useDisconnect();
  const check = useCheck();
  const saveSettings = useSaveSettings();
  const revoke = useRevokeAll();
  const [signedOut, setSignedOut] = useState(false);

  if (signedOut) return <p>Signed out of all browsers. Open the app with scripts/start.sh</p>;
  if (accounts.isPending || setup.isPending)
    return <p className={styles.muted}>Loading accounts</p>;
  if (accounts.isError || setup.isError) return <p role="alert">Could not load the accounts.</p>;

  const { github, jira } = accounts.data;
  const configured = {
    github: setup.data.github.configured,
    atlassian: setup.data.atlassian.configured,
  };
  const failure = [disconnect, check, saveSettings, revoke].find((m) => m.isError)?.error;

  const pickSite = (siteId: string) => {
    if (!settings.data) return;
    saveSettings.mutate({ ...settings.data, jiraSiteId: siteId });
  };

  return (
    <section aria-labelledby="accounts-title">
      <h2 id="accounts-title" className={styles.subtitle}>
        Connected accounts
      </h2>

      <div className={styles.row}>
        <div className={styles.rowText}>
          <strong>GitHub</strong>
          {github.connected ? (
            <span className={styles.ok}>
              Connected as {github.login} ({github.method === 'pat' ? 'access token' : 'OAuth'})
            </span>
          ) : (
            <span className={styles.muted}>Not connected</span>
          )}
          {github.warning && <span className={styles.warn}>{github.warning}</span>}
        </div>
        <div className={styles.actions}>
          {github.connected ? (
            <>
              <button className={styles.button} onClick={() => check.mutate('github')}>
                Check GitHub
              </button>
              <button className={styles.button} onClick={() => disconnect.mutate('github')}>
                Disconnect GitHub
              </button>
            </>
          ) : (
            <Connect
              provider="github"
              label="Connect GitHub"
              configured={configured.github}
              primary
            />
          )}
        </div>
      </div>

      <div className={styles.row}>
        <div className={styles.rowText}>
          <strong>Jira</strong>
          {jira.needsReconnect && (
            <span className={styles.warn}>
              The Jira authorization expired. Reconnect to continue.
            </span>
          )}
          {jira.connected && (
            <span className={styles.ok}>
              Connected to {jira.sites.length} site{jira.sites.length === 1 ? '' : 's'}
            </span>
          )}
          {!jira.connected && !jira.needsReconnect && (
            <span className={styles.muted}>Not connected</span>
          )}
          {jira.connected && jira.sites.length > 1 && (
            <label className={styles.field}>
              Jira site
              <select
                value={jira.selectedSiteId ?? ''}
                onChange={(e) => pickSite(e.target.value)}
                disabled={!settings.data || saveSettings.isPending}
              >
                {jira.selectedSiteId === null && <option value="">Choose a site</option>}
                {jira.sites.map((site) => (
                  <option key={site.id} value={site.id}>
                    {site.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {jira.connected && jira.sites.length === 1 && jira.sites[0] && (
            <span className={styles.muted}>{jira.sites[0].name}</span>
          )}
        </div>
        <div className={styles.actions}>
          {jira.needsReconnect && (
            <Connect
              provider="atlassian"
              label="Reconnect Jira"
              configured={configured.atlassian}
              primary
            />
          )}
          {jira.connected && (
            <button className={styles.button} onClick={() => check.mutate('atlassian')}>
              Check Jira
            </button>
          )}
          {(jira.connected || jira.needsReconnect) && (
            <button className={styles.button} onClick={() => disconnect.mutate('atlassian')}>
              Disconnect Jira
            </button>
          )}
          {!jira.connected && !jira.needsReconnect && (
            <Connect
              provider="atlassian"
              label="Connect Jira"
              configured={configured.atlassian}
              primary
            />
          )}
        </div>
      </div>

      <div className={styles.row}>
        <div className={styles.rowText}>
          <strong>Browsers</strong>
          <span className={styles.muted}>Ends every session, including this one.</span>
        </div>
        <button
          className={styles.button}
          onClick={() => revoke.mutate(undefined, { onSuccess: () => setSignedOut(true) })}
        >
          Sign out all browsers
        </button>
      </div>

      <div className={styles.row}>
        <a className={styles.link} href="/setup/">
          Setup
        </a>
      </div>
      {failure !== undefined && failure !== null && (
        <p role="alert" className={styles.error}>
          {problemText(failure)}
        </p>
      )}
    </section>
  );
}
