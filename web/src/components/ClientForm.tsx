'use client';

import { useState, type FormEvent } from 'react';
import styles from '@/app/page.module.css';
import { ApiError, type Provider } from '@/api/client';
import { useSaveClient } from '@/api/queries';
import { appInstallUrl } from '@/lib/githubApp';
import { describeProblem } from '@/lib/problems';

interface Props {
  provider: Provider;
  title: string;
  configured: boolean;
  callbackUrl: string;
  /** Show the GitHub App slug field. */
  withSlug?: boolean;
  /** The install link the server built from the saved slug. */
  installUrl?: string;
}

export function ClientForm({
  provider,
  title,
  configured,
  callbackUrl,
  withSlug,
  installUrl,
}: Props) {
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
  const [appSlug, setAppSlug] = useState('');
  const save = useSaveClient();

  const link = withSlug ? appInstallUrl(installUrl) : null;

  const submit = (event: FormEvent) => {
    event.preventDefault();
    const slug = appSlug.trim();
    save.mutate(
      { provider, clientId, clientSecret, ...(withSlug && slug ? { appSlug: slug } : {}) },
      {
        onSuccess: () => {
          setClientId('');
          setClientSecret('');
          setAppSlug('');
        },
      },
    );
  };

  return (
    <section aria-labelledby={`${provider}-client`}>
      <h2 id={`${provider}-client`} className={styles.subtitle}>
        {title}
      </h2>
      <p className={styles.muted}>
        Callback URL to register: <code className={styles.code}>{callbackUrl}</code>
      </p>
      <p className={configured ? styles.ok : styles.muted}>
        {configured ? 'Client credentials saved' : 'Not configured'}
      </p>
      <form onSubmit={submit}>
        <label className={styles.field}>
          Client ID
          <input
            name="clientId"
            autoComplete="off"
            required
            maxLength={256}
            value={clientId}
            onChange={(e) => setClientId(e.target.value)}
          />
        </label>
        <label className={styles.field}>
          Client secret
          <input
            name="clientSecret"
            type="password"
            autoComplete="off"
            required
            maxLength={512}
            value={clientSecret}
            onChange={(e) => setClientSecret(e.target.value)}
          />
        </label>
        {withSlug && (
          <label className={styles.field}>
            App slug (optional)
            <input
              name="appSlug"
              autoComplete="off"
              maxLength={100}
              pattern="[a-z0-9][a-z0-9\-]*"
              title="Lowercase letters, digits and dashes"
              value={appSlug}
              onChange={(e) => setAppSlug(e.target.value)}
            />
            <span className={styles.muted}>
              From the app&apos;s public link, github.com/apps/&lt;slug&gt;
            </span>
          </label>
        )}
        <button className={styles.button} type="submit" disabled={save.isPending}>
          Save credentials
        </button>
        {save.isError && (
          <p role="alert" className={styles.error}>
            {describeProblem(save.error instanceof ApiError ? save.error.code : undefined)}
          </p>
        )}
        {save.isSuccess && <p className={styles.ok}>Saved</p>}
      </form>
      {link && (
        <p>
          <a className={styles.link} href={link} target="_blank" rel="noopener noreferrer">
            Install on repositories
          </a>
          <span className={styles.muted}>
            {' '}
            An organization owner picks the repositories the app can see.
          </span>
        </p>
      )}
      {configured && (
        <p>
          <a className={`${styles.link} ${styles.primary}`} href={`/oauth/${provider}/start`}>
            {provider === 'github' ? 'Connect GitHub' : 'Connect Jira'}
          </a>
        </p>
      )}
    </section>
  );
}
