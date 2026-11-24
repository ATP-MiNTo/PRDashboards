'use client';

import Link from 'next/link';
import { ApiError } from '@/api/client';
import { useSession } from '@/api/queries';
import { AccountsPanel } from '@/components/AccountsPanel';
import styles from '../page.module.css';

export default function Accounts() {
  const session = useSession();

  return (
    <main className={styles.main}>
      <section className={`${styles.card} ${styles.wide}`}>
        <h1 className={styles.title}>PR Dashboards</h1>
        {session.isPending && <p className={styles.muted}>Loading</p>}
        {session.isSuccess && (
          <div className={styles.stack}>
            <div>
              <p>Signed in</p>
              <p className={styles.muted}>Version {session.data.version}</p>
              <p>
                <Link href="/">Back to the dashboard</Link>
              </p>
            </div>
            <AccountsPanel />
          </div>
        )}
        {session.isError &&
          (session.error instanceof ApiError && session.error.status === 401 ? (
            <p>Open the app with scripts/start.sh</p>
          ) : (
            <p>Could not reach the server. Run scripts/start.sh again.</p>
          ))}
      </section>
    </main>
  );
}
