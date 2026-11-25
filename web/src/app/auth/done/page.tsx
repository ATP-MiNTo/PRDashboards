'use client';

import Link from 'next/link';
import { useSyncExternalStore } from 'react';
import { isProvider, type FinishResult } from '@/lib/oauthFinish';
import styles from '../../page.module.css';

const NAMES = { github: 'GitHub', atlassian: 'Jira' } as const;
const RESULTS: readonly FinishResult[] = [
  'ok',
  'denied',
  'invalid_state',
  'not_configured',
  'signed_out',
  'error',
];

interface Outcome {
  result: FinishResult;
  name: string;
}

function readOutcome(search: string): Outcome {
  const q = new URLSearchParams(search);
  const provider = q.get('provider');
  const result = q.get('result') as FinishResult | null;
  return {
    result: result !== null && RESULTS.includes(result) ? result : 'error',
    name: isProvider(provider) ? NAMES[provider] : 'The provider',
  };
}

function message({ result, name }: Outcome): string {
  switch (result) {
    case 'ok':
      return `${name} is connected.`;
    case 'denied':
      return `${name} access was not granted.`;
    case 'invalid_state':
      return 'The sign-in link expired or was already used. Start again.';
    case 'not_configured':
      return `Set up the ${name} app first.`;
    case 'signed_out':
      return 'Your session ended. Open the app with scripts/start.sh';
    default:
      return `Could not connect ${name}. Try again.`;
  }
}

const subscribe = () => () => {};
const clientSearch = () => window.location.search;
const serverSearch = () => null;

export default function Done() {
  const search = useSyncExternalStore(subscribe, clientSearch, serverSearch);
  const outcome = search === null ? null : readOutcome(search);

  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>PR Dashboards</h1>
        {outcome === null ? (
          <p className={styles.muted}>Loading</p>
        ) : (
          <>
            <p role="status" className={outcome.result === 'ok' ? styles.ok : undefined}>
              {message(outcome)}
            </p>
            {outcome.result === 'not_configured' && (
              <p>
                <Link href="/setup/">Go to setup</Link>
              </p>
            )}
            <p>
              <Link href="/accounts/">Back to accounts</Link>
            </p>
          </>
        )}
      </section>
    </main>
  );
}
