'use client';

import { useState, type FormEvent } from 'react';
import styles from '@/app/page.module.css';
import { ApiError } from '@/api/client';
import { useSavePat } from '@/api/queries';
import { describeProblem } from '@/lib/problems';

export function PatForm() {
  const [token, setToken] = useState('');
  const save = useSavePat();

  const submit = (event: FormEvent) => {
    event.preventDefault();
    save.mutate(token, { onSuccess: () => setToken('') });
  };

  return (
    <section aria-labelledby="pat-title">
      <h2 id="pat-title" className={styles.subtitle}>
        GitHub access token
      </h2>
      <p className={styles.muted}>
        An alternative to OAuth. Classic tokens need the repo and read:org scopes. Fine-grained
        tokens only see the repositories they were granted.
      </p>
      <form onSubmit={submit}>
        <label className={styles.field}>
          Personal access token
          <input
            name="token"
            type="password"
            autoComplete="off"
            required
            maxLength={512}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </label>
        <button className={styles.button} type="submit" disabled={save.isPending}>
          Validate and save
        </button>
        {save.isError && (
          <p role="alert" className={styles.error}>
            {describeProblem(save.error instanceof ApiError ? save.error.code : undefined)}
          </p>
        )}
        {save.isSuccess && (
          <>
            <p className={styles.ok}>Connected as {save.data.login}</p>
            {save.data.warning && <p className={styles.warn}>{save.data.warning}</p>}
          </>
        )}
      </form>
    </section>
  );
}
