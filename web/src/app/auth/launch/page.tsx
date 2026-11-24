'use client';

import { useEffect, useRef, useState } from 'react';
import { ApiError, createSession } from '@/api/client';
import { runLaunch, type LaunchOutcome } from '@/lib/launch';
import styles from '../../page.module.css';

const MESSAGES: Record<Exclude<LaunchOutcome, 'signed-in'>, string> = {
  'missing-token': 'This link has no launch token. Open the app with scripts/start.sh',
  rejected: 'The launch link expired or was already used. Open the app with scripts/start.sh',
  failed: 'Could not reach the server. Run scripts/start.sh again.',
};

export default function Launch() {
  const [outcome, setOutcome] = useState<LaunchOutcome | null>(null);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void runLaunch({
      hash: window.location.hash,
      clearHash: () =>
        window.history.replaceState(null, '', window.location.pathname + window.location.search),
      createSession,
      isRejection: (error) => error instanceof ApiError && error.status === 401,
      goHome: () => window.location.replace('/'),
    }).then(setOutcome);
  }, []);

  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>PR Dashboards</h1>
        <p className={styles.muted}>
          {outcome === null || outcome === 'signed-in' ? 'Signing in' : MESSAGES[outcome]}
        </p>
      </section>
    </main>
  );
}
