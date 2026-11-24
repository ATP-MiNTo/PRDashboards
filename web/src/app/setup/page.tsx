'use client';

import Link from 'next/link';
import { ApiError } from '@/api/client';
import { useSetup } from '@/api/queries';
import { ClientForm } from '@/components/ClientForm';
import { PatForm } from '@/components/PatForm';
import styles from '../page.module.css';

export default function Setup() {
  const setup = useSetup();

  return (
    <main className={styles.main}>
      <section className={`${styles.card} ${styles.wide}`}>
        <h1 className={styles.title}>Setup</h1>
        <p className={styles.muted}>
          Register one OAuth app per provider, then paste its client ID and secret here. They are
          stored in your keychain and never leave this machine.
        </p>
        {setup.isPending && <p className={styles.muted}>Loading</p>}
        {setup.isError &&
          (setup.error instanceof ApiError && setup.error.status === 401 ? (
            <p>Open the app with scripts/start.sh</p>
          ) : (
            <p role="alert">Could not load the setup state.</p>
          ))}
        {setup.isSuccess && (
          <div className={styles.stack}>
            <ClientForm
              provider="github"
              title="GitHub OAuth App"
              configured={setup.data.github.configured}
              callbackUrl={setup.data.github.callbackUrl}
            />
            <PatForm />
            <ClientForm
              provider="atlassian"
              title="Atlassian OAuth 2.0 (3LO) app"
              configured={setup.data.atlassian.configured}
              callbackUrl={setup.data.atlassian.callbackUrl}
            />
          </div>
        )}
        <p>
          <Link href="/">Back to the dashboard</Link>
        </p>
      </section>
    </main>
  );
}
