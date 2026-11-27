'use client';

import { useId, useState, type FormEvent } from 'react';
import type { Provider } from '@/api/client';
import { ApiError } from '@/api/client';
import type { GithubToken } from '@/api/client';
import {
  useAccounts,
  useAddToken,
  useCheck,
  useCheckToken,
  useDisconnect,
  useRemoveToken,
  useRenameToken,
  useRepoTokens,
  useRepos,
  useRevokeAll,
  useSaveSettings,
  useSettings,
  useSetup,
} from '@/api/queries';
import {
  SOURCE_NAME,
  TOKEN_NAME_MAX,
  kindOf,
  siteHost,
  sourceOf,
  tokenBadgeText,
  tokenNameProblem,
  tokenOfRepo,
} from '@/lib/accounts';
import { describeProblem } from '@/lib/problems';
import { InfoTip } from '../InfoTip/InfoTip';
import { Modal } from '../Modal/Modal';
import { ModalHead } from '../Modal/ModalHead';
import styles from './AccountsModal.module.css';

const problemText = (e: unknown) => describeProblem(e instanceof ApiError ? e.code : undefined);

/** A link that looks like a button: the sign-in flows start at a server route, not an API call. */
function Start({
  provider,
  configured,
  label,
  size,
  name,
}: {
  provider: Provider;
  configured: boolean;
  label: string;
  size: 'xs' | 'sm';
  /** The accessible name, when the visible label alone does not say what it reconnects. */
  name?: string;
}) {
  return configured ? (
    <a className={styles[size]} href={`/oauth/${provider}/start`} aria-label={name}>
      {label}
    </a>
  ) : (
    <a className={styles[size]} href="/setup/">
      Set up
    </a>
  );
}

const NEEDS_APP_SIGN_IN = 'The GitHub authorization expired. Reconnect to continue.';
const NEEDS_NEW_TOKEN = 'GitHub no longer accepts this token. Remove it and add a new one.';

/** One stored token: its name (renamed in place), kind, login and state, with Reconnect, Check and Remove. */
function TokenRow({
  token,
  primary,
  last,
  configured,
}: {
  token: GithubToken;
  primary: boolean;
  last: boolean;
  configured: boolean;
}) {
  const rename = useRenameToken();
  const remove = useRemoveToken();
  const check = useCheckToken();
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(token.name);
  const [confirming, setConfirming] = useState(false);
  const [checked, setChecked] = useState(false);
  const kind = kindOf(token);
  const viaSignIn = token.method !== 'pat';
  const nameProblem = tokenNameProblem(name);
  const failure = [rename, remove, check].find((m) => m.isError)?.error;

  const startEdit = () => {
    setName(token.name);
    rename.reset();
    setEditing(true);
  };
  const stopEdit = () => {
    rename.reset();
    setEditing(false);
  };
  const save = (e: FormEvent) => {
    e.preventDefault();
    const next = name.trim();
    if (nameProblem) return;
    if (next === token.name) return stopEdit();
    rename.mutate({ id: token.id, name: next }, { onSuccess: () => setEditing(false) });
  };

  return (
    <li className={styles.token}>
      <div className={styles.tokenHead}>
        {editing ? (
          <form className={styles.form} onSubmit={save}>
            <input
              className={styles.field}
              type="text"
              autoComplete="off"
              autoFocus
              maxLength={TOKEN_NAME_MAX}
              aria-label={`Name of ${token.name}`}
              aria-invalid={nameProblem !== null}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Escape') {
                  e.stopPropagation();
                  stopEdit();
                }
              }}
            />
            <button
              type="submit"
              className={styles.xs}
              disabled={rename.isPending || nameProblem !== null}
            >
              Save name
            </button>
            <button type="button" className={styles.xs} onClick={stopEdit}>
              Cancel
            </button>
          </form>
        ) : (
          <span className={styles.tokenName}>
            <b className={styles.tokenTitle}>{token.name}</b>
            <button
              type="button"
              className={styles.xs}
              aria-label={`Rename ${token.name}`}
              onClick={startEdit}
            >
              Rename
            </button>
          </span>
        )}
        <span className={styles.badges}>
          <span className={`${styles.badge} ${styles.ghost}`}>{SOURCE_NAME[kind]}</span>
          {primary && (
            <>
              <span className={`${styles.badge} ${styles.ghost}`}>Primary</span>
              <InfoTip
                tip="The primary token is the first one. It wins a tie when several tokens see the same repository."
                label="About the primary token"
              />
            </>
          )}
          {token.needsReconnect ? (
            <span className={`${styles.badge} ${styles.warn}`}>Needs reconnect</span>
          ) : (
            <span className={`${styles.badge} ${styles.good}`}>Connected</span>
          )}
        </span>
      </div>
      {editing && nameProblem && name !== '' && <p className={styles.problem}>{nameProblem}</p>}
      <div className={styles.sub}>{token.login}</div>
      {token.needsReconnect ? (
        <p className={styles.attention}>{viaSignIn ? NEEDS_APP_SIGN_IN : NEEDS_NEW_TOKEN}</p>
      ) : (
        token.warning && <p className={styles.attention}>{token.warning}</p>
      )}
      {checked && !check.isPending && !token.needsReconnect && (
        <div className={styles.sub}>Checked just now. GitHub accepts this token.</div>
      )}
      <div className={styles.actions}>
        {viaSignIn && (
          <Start
            provider="github"
            configured={configured}
            label="Reconnect"
            name={`Reconnect ${token.name}`}
            size="xs"
          />
        )}
        <button
          type="button"
          className={styles.xs}
          aria-label={`Check ${token.name}`}
          disabled={check.isPending}
          onClick={() => {
            setChecked(false);
            check.mutate(token.id, { onSuccess: () => setChecked(true) });
          }}
        >
          Check
        </button>
        {!confirming && (
          <button
            type="button"
            className={styles.xs}
            aria-label={`Remove ${token.name}`}
            onClick={() => setConfirming(true)}
          >
            Remove
          </button>
        )}
      </div>
      {confirming && (
        <div className={styles.confirm} role="group" aria-label={`Remove ${token.name}?`}>
          <span>
            {last
              ? `Remove ${token.name}? It is your only token, so this disconnects GitHub.`
              : `Remove ${token.name}? Repositories only it can see leave the list.`}
          </span>
          <button
            type="button"
            className={`${styles.xs} ${styles.error}`}
            disabled={remove.isPending}
            onClick={() => remove.mutate(token.id, { onError: () => setConfirming(false) })}
          >
            Confirm remove
          </button>
          <button type="button" className={styles.xs} onClick={() => setConfirming(false)}>
            Keep
          </button>
        </div>
      )}
      {failure !== undefined && failure !== null && (
        <p className={styles.problem} role="alert">
          {problemText(failure)}
        </p>
      )}
    </li>
  );
}

/** Adds a token with an optional name. The token is never shown again. */
function AddTokenForm({ count, limit }: { count: number; limit: number }) {
  const add = useAddToken();
  const [name, setName] = useState('');
  const [token, setToken] = useState('');
  const full = count >= limit;
  const nameProblem = name.trim() === '' ? null : tokenNameProblem(name);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    add.mutate(trimmed === '' ? { token } : { token, name: trimmed }, {
      onSuccess: () => {
        setToken('');
        setName('');
      },
    });
  };
  return (
    <form className={styles.addForm} aria-label="Add token" onSubmit={submit}>
      <div className={styles.name}>Add token</div>
      <div className={styles.form}>
        <input
          className={styles.field}
          type="text"
          autoComplete="off"
          maxLength={TOKEN_NAME_MAX}
          placeholder="Name (optional)"
          aria-label="Token name (optional)"
          aria-invalid={nameProblem !== null}
          disabled={full}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <input
          className={styles.field}
          type="password"
          autoComplete="off"
          required
          maxLength={512}
          placeholder="Paste the token"
          aria-label="Personal access token"
          disabled={full}
          value={token}
          onChange={(e) => setToken(e.target.value)}
        />
        <button
          type="submit"
          className={styles.xs}
          disabled={full || add.isPending || token === '' || nameProblem !== null}
        >
          Validate and add
        </button>
      </div>
      {nameProblem && <p className={styles.problem}>{nameProblem}</p>}
      {full && <p className={styles.sub}>You can connect up to {limit} tokens</p>}
      {add.isError && (
        <p className={styles.problem} role="alert">
          {problemText(add.error)}
        </p>
      )}
      {add.isSuccess && !add.isPending && (
        <p className={styles.sub} role="status">
          Added {add.data.name}.
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
  const tokens = accounts.data?.githubTokens ?? [];
  const limit = accounts.data?.githubTokenLimit ?? 10;
  const repoTokens = useRepoTokens().data ?? {};
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
                <h4 className={styles.name}>
                  GitHub tokens
                  {github.connected && (
                    <span className={styles.count}>
                      {tokens.length} of {limit}
                      <span className={styles.sr}> tokens connected</span>
                    </span>
                  )}
                </h4>
                <div className={styles.sub}>
                  {github.connected
                    ? 'A fine-grained token works for one owner only. Add one token per owner to read and act on all your repositories.'
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
              {github.connected && (
                <button
                  type="button"
                  className={styles.xs}
                  disabled={disconnect.isPending}
                  onClick={() => disconnect.mutate('github')}
                >
                  Disconnect GitHub
                </button>
              )}
            </div>

            {github.connected && tokens.length > 0 && (
              <>
                <ul className={styles.tokens} aria-label="Connected tokens">
                  {tokens.map((t, i) => (
                    <TokenRow
                      key={t.id}
                      token={t}
                      primary={i === 0}
                      last={tokens.length === 1}
                      configured={configured.github}
                    />
                  ))}
                </ul>
                <AddTokenForm count={tokens.length} limit={limit} />
              </>
            )}

            {github.connected && source && list.length > 0 && (
              <div className={styles.tableWrap}>
                <table className={styles.table}>
                  <thead>
                    <tr>
                      <th scope="col">Repository</th>
                      <th scope="col">Source</th>
                    </tr>
                  </thead>
                  <tbody>
                    {list.map((r) => {
                      const ref = tokenOfRepo(repoTokens[r.id], tokens);
                      const kind = ref?.kind ?? source;
                      return (
                        <tr key={r.id}>
                          <td>{r.nameWithOwner}</td>
                          <td>
                            <span
                              className={`${styles.badge} ${styles.source} ${kind === 'app' || kind === 'oauth' ? styles.good : styles.warn}`}
                            >
                              {ref ? tokenBadgeText(ref) : SOURCE_NAME[source]}
                            </span>
                          </td>
                        </tr>
                      );
                    })}
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
