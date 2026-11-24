'use client';

import { useEffect, useState } from 'react';
import { ApiError, getSession } from '@/api/client';
import styles from './page.module.css';

type State =
  | { kind: 'loading' }
  | { kind: 'signed-in'; version: string }
  | { kind: 'signed-out' }
  | { kind: 'error' };

export default function Home() {
  const [state, setState] = useState<State>({ kind: 'loading' });

  useEffect(() => {
    let active = true;
    getSession().then(
      (session) => active && setState({ kind: 'signed-in', version: session.version }),
      (error: unknown) =>
        active &&
        setState(
          error instanceof ApiError && error.status === 401
            ? { kind: 'signed-out' }
            : { kind: 'error' },
        ),
    );
    return () => {
      active = false;
    };
  }, []);

  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>PR Dashboards</h1>
        {state.kind === 'loading' && <p className={styles.muted}>Loading</p>}
        {state.kind === 'signed-in' && (
          <>
            <p>Signed in</p>
            <p className={styles.muted}>Version {state.version}</p>
          </>
        )}
        {state.kind === 'signed-out' && <p>Open the app with scripts/start.sh</p>}
        {state.kind === 'error' && <p>Could not reach the server. Run scripts/start.sh again.</p>}
      </section>
    </main>
  );
}
