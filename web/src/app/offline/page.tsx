import styles from '../page.module.css';

export const metadata = { title: 'Offline - PR Dashboards' };

export default function Offline() {
  return (
    <main className={styles.main}>
      <section className={styles.card}>
        <h1 className={styles.title}>PR Dashboards is not running</h1>
        <p className={styles.muted}>
          The app runs on this computer. Start it with <code>scripts/start.sh</code>, then reload
          this page.
        </p>
      </section>
    </main>
  );
}
