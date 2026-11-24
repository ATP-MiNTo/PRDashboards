'use client';

import { useEffect, useRef } from 'react';
import { ApiError, completeOAuth } from '@/api/client';
import { runFinish } from '@/lib/oauthFinish';
import styles from '../../page.module.css';

export default function Finish() {
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    void runFinish({
      search: window.location.search,
      clearQuery: () => window.history.replaceState(null, '', window.location.pathname),
      complete: completeOAuth,
      errorCode: (error) =>
        error instanceof ApiError ? { status: error.status, code: error.code } : null,
      navigate: (url) => window.location.replace(url),
    });
  }, []);

  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>PR Dashboards</h1>
        <p className={styles.muted}>Finishing sign-in</p>
      </section>
    </main>
  );
}
