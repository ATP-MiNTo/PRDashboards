import Link from 'next/link';
import styles from './page.module.css';

export default function NotFound() {
  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>Page not found</h1>
        <p className={styles.muted}>
          <Link href="/">Back to the dashboard</Link>
        </p>
      </section>
    </main>
  );
}
