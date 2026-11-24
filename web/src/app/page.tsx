'use client';

import { ApiError } from '@/api/client';
import { useSession } from '@/api/queries';
import { CommandTable } from '@/components/CommandTable/CommandTable';
import styles from './page.module.css';

export default function Home() {
  const session = useSession();

  if (session.isSuccess) return <CommandTable />;
  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>PR Dashboards</h1>
        {session.isPending && <p className={styles.muted}>Loading</p>}
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
