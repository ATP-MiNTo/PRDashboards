'use client';

import { useId, useState, type FormEvent } from 'react';
import type { Provider } from '@/api/client';
import { ApiError } from '@/api/client';
import {
  useAccounts,
  useCheck,
  useDisconnect,
  useRepos,
  useRevokeAll,
  useSavePat,
  useSaveSettings,
  useSettings,
  useSetup,
} from '@/api/queries';
import { SOURCE_NAME, SOURCE_WHY, siteHost, sourceOf, type Source } from '@/lib/accounts';
import { describeProblem } from '@/lib/problems';
import { Modal } from '../Modal/Modal';
import { ModalHead } from '../Modal/ModalHead';
import styles from './AccountsModal.module.css';

const problemText = (e: unknown) => describeProblem(e instanceof ApiError ? e.code : undefined);

/** What the person is told about how GitHub is connected. */
const SOURCE_ABOUT: Record<Source, string> = {
  app: 'Signed in through the GitHub App. The token is kept in the encrypted vault.',
  oauth: 'Signed in with OAuth. The token is kept in the encrypted vault.',
  'fine-grained':
    'A personal access token, kept in the encrypted vault. It only sees the repositories it was granted.',
  classic:
    'A personal access token, kept in the encrypted vault. It sees every repository you can.',
};

/** A link that looks like a button: the sign-in flows start at a server route, not an API call. */
function Start({
  provider,
  configured,
  label,
  size,
}: {
  provider: Provider;
  configured: boolean;
  label: string;
  size: 'xs' | 'sm';
}) {
  return configured ? (
    <a className={styles[size]} href={`/oauth/${provider}/start`}>
      {label}
    </a>
  ) : (
    <a className={styles[size]} href="/setup/">
      Set up
    </a>
  );
}

/** Replaces the stored token: the same call as the Setup page's form. The token is never shown again. */
function TokenForm({ onDone }: { onDone: () => void }) {
  const [token, setToken] = useState('');
  const save = useSavePat();
  const submit = (e: FormEvent) => {
    e.preventDefault();
    save.mutate(token, {
      onSuccess: () => {
        setToken('');
        onDone();
      },
    });
  };
  return (
    <form className={styles.form} onSubmit={submit}>
      <input
        className={styles.token}
        type="password"
        autoComplete="off"
        required
        maxLength={512}
        placeholder="Paste the new token"
        aria-label="Personal access token"
        value={token}
        onChange={(e) => setToken(e.target.value)}
      />
      <button type="submit" className={styles.xs} disabled={save.isPending || token === ''}>
        Validate and save
      </button>
      {save.isError && (
        <p className={styles.problem} role="alert">
          {problemText(save.error)}
        </p>
      )}
    </form>
  );
}

/**
 * The Accounts dialog, opened by "Manage accounts" in the sidebar: how GitHub is connected (the App
 * or a token) with Reconnect, Replace token and Disconnect, the table that says which one fetches
 * each repository, Jira with its site, and the browsers with "Sign out everywhere".
 */
export function AccountsModal({ onClose }: { onClose: () => void }) {
  const titleId = useId();
  const accounts = useAccounts();
  const setup = useSetup();
  const repos = useRepos();
  const settings = useSettings();
  const saveSettings = useSaveSettings();
  const disconnect = useDisconnect();
  const check = useCheck();
  const revoke = useRevokeAll();
  const [replacing, setReplacing] = useState(false);
  const [checked, setChecked] = useState<Provider | null>(null);

  const github = accounts.data?.github;
  const jira = accounts.data?.jira;
  const source = github ? sourceOf(github) : null;
  const configured = {
    github: setup.data?.github.configured ?? false,
    atlassian: setup.data?.atlassian.configured ?? false,
  };
  const failure = [disconnect, check, saveSettings, revoke].find((m) => m.isError)?.error;
  const site = jira?.sites.find((s) => s.id === jira.selectedSiteId) ?? jira?.sites[0];
  const host = site ? (siteHost(site.url) ?? site.name) : null;
  const isToken = source === 'fine-grained' || source === 'classic';
  const list = repos.data?.repos ?? [];

  const runCheck = (p: Provider) =>
    check.mutate(p, { onSuccess: () => setChecked(p), onError: () => setChecked(null) });

  return (
    <Modal labelledBy={titleId} onClose={onClose} wide>
      <ModalHead titleId={titleId} title="Accounts" onClose={onClose} />
      <div className={styles.stack}>
        {(accounts.isPending || setup.isPending) && (
          <p className={styles.muted}>Loading accounts</p>
        )}
        {(accounts.isError || setup.isError) && (
          <p className={styles.problem} role="alert">
            Could not load the accounts.
          </p>
        )}

        {github && (
          <section className={styles.card} aria-label="GitHub">
            <div className={styles.row}>
              <div>
                <div className={styles.name}>
                  GitHub{github.connected && github.login ? ` · ${github.login}` : ''}
                </div>
                <div className={styles.sub}>
                  {source
                    ? `${SOURCE_ABOUT[source]} Each repository below says which one fetches its pull requests.`
                    : 'Not connected. Connect with the GitHub App or paste a personal access token.'}
                </div>
              </div>
              {!github.connected && (
                <Start
                  provider="github"
                  configured={configured.github}
                  label="Connect GitHub"
                  size="sm"
                />
              )}
            </div>

            {github.connected && source && (
              <div className={styles.pair}>
                <div className={styles.inner}>
                  <div className={styles.innerHead}>
                    <b>{SOURCE_NAME[source]}</b>
                    {github.needsReconnect ? (
                      <span className={`${styles.badge} ${styles.warn}`}>Needs reconnect</span>
                    ) : (
                      <span className={`${styles.badge} ${styles.good}`}>Connected</span>
                    )}
                  </div>
                  <div className={styles.sub}>
                    {github.needsReconnect
                      ? 'The GitHub authorization expired. Reconnect to continue.'
                      : checked === 'github'
                        ? 'Checked just now. GitHub accepts the connection.'
                        : (github.warning ?? SOURCE_ABOUT[source])}
                  </div>
                  <div className={styles.actions}>
                    {!isToken && (
                      <Start
                        provider="github"
                        configured={configured.github}
                        label="Reconnect GitHub"
                        size="xs"
                      />
                    )}
                    {isToken && (
                      <button
                        type="button"
                        className={styles.xs}
                        aria-expanded={replacing}
                        onClick={() => setReplacing(!replacing)}
                      >
                        Replace token
                      </button>
                    )}
                    <button
                      type="button"
                      className={styles.xs}
                      disabled={check.isPending}
                      onClick={() => runCheck('github')}
                    >
                      Check GitHub
                    </button>
                    <button
                      type="button"
                      className={styles.xs}
                      disabled={disconnect.isPending}
                      onClick={() => disconnect.mutate('github')}
                    >
                      Disconnect GitHub
                    </button>
                  </div>
                  {isToken && replacing && <TokenForm onDone={() => setReplacing(false)} />}
                </div>
              </div>
            )}

            {github.connected && source && list.length > 0 && (
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th scope="col">Repository</th>
                      <th scope="col">Source</th>
                      <th scope="col">Why</th>
                    </tr>
                  </thead>
                  <tbody>
                    {list.map((r) => (
                      <tr key={r.id}>
                        <td>{r.nameWithOwner}</td>
                        <td>
                          <span
                            className={`${styles.badge} ${source === 'app' || source === 'oauth' ? styles.good : styles.warn}`}
                          >
                            {SOURCE_NAME[source]}
                          </span>
                        </td>
                        <td className={styles.why}>{SOURCE_WHY[source]}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        )}

        {jira && (
          <section className={`${styles.card} ${styles.row}`} aria-label="Jira">
            <div>
              <div className={styles.name}>Jira{host ? ` · ${host}` : ''}</div>
              <div className={styles.sub}>
                {jira.needsReconnect
                  ? 'The Jira authorization expired. Reconnect to continue.'
                  : jira.connected
                    ? checked === 'atlassian'
                      ? 'Checked just now. Atlassian accepts the connection.'
                      : 'Atlassian sign-in, can read and move issues.'
                    : 'Not connected. Connect Jira to link tickets and move them.'}
              </div>
              {jira.connected && jira.sites.length > 1 && (
                <select
                  className={styles.site}
                  aria-label="Jira site"
                  value={jira.selectedSiteId ?? ''}
                  disabled={!settings.data || saveSettings.isPending}
                  onChange={(e) =>
                    settings.data &&
                    saveSettings.mutate({ ...settings.data, jiraSiteId: e.target.value })
                  }
                >
                  {jira.selectedSiteId === null && <option value="">Choose a site</option>}
                  {jira.sites.map((s) => (
                    <option key={s.id} value={s.id}>
                      {siteHost(s.url) ?? s.name}
                    </option>
                  ))}
                </select>
              )}
            </div>
            <div className={styles.actions}>
              {(jira.connected || jira.needsReconnect) && (
                <>
                  <Start
                    provider="atlassian"
                    configured={configured.atlassian}
                    label="Reconnect"
                    size="sm"
                  />
                  {jira.connected && (
                    <button
                      type="button"
                      className={styles.sm}
                      disabled={check.isPending}
                      onClick={() => runCheck('atlassian')}
                    >
                      Check Jira
                    </button>
                  )}
                  <button
                    type="button"
                    className={styles.sm}
                    disabled={disconnect.isPending}
                    onClick={() => disconnect.mutate('atlassian')}
                  >
                    Disconnect Jira
                  </button>
                </>
              )}
              {!jira.connected && !jira.needsReconnect && (
                <Start
                  provider="atlassian"
                  configured={configured.atlassian}
                  label="Connect Jira"
                  size="sm"
                />
              )}
            </div>
          </section>
        )}

        <section className={`${styles.card} ${styles.row}`} aria-label="Browsers">
          <div>
            <div className={styles.name}>Browsers</div>
            <div className={styles.sub}>
              {revoke.isSuccess
                ? 'Signed out everywhere. Open the launch link from scripts/start.sh to sign in again.'
                : 'Ends every session, including this one.'}
            </div>
          </div>
          <button
            type="button"
            className={`${styles.sm} ${styles.error}`}
            disabled={revoke.isPending || revoke.isSuccess}
            onClick={() => revoke.mutate(undefined)}
          >
            Sign out everywhere
          </button>
        </section>

        {failure !== undefined && failure !== null && (
          <p className={styles.problem} role="alert">
            {problemText(failure)}
          </p>
        )}
        <p className={styles.foot}>
          Client credentials and the token form are on the <a href="/setup/">Setup</a> page.
        </p>
      </div>
    </Modal>
  );
}
