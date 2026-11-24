'use client';

import { useState, type FormEvent } from 'react';
import styles from '@/app/page.module.css';
import { ApiError, type Provider } from '@/api/client';
import { useSaveClient } from '@/api/queries';
import { describeProblem } from '@/lib/problems';

interface Props {
  provider: Provider;
  title: string;
  configured: boolean;
  callbackUrl: string;
}

export function ClientForm({ provider, title, configured, callbackUrl }: Props) {
  const [clientId, setClientId] = useState('');
  const [clientSecret, setClientSecret] = useState('');
  const save = useSaveClient();

  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate(
      { provider, clientId, clientSecret },
      {
        onSuccess: () => {
          setClientId('');
          setClientSecret('');
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
