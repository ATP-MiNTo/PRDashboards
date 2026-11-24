import { useActivity, useDismissRetry, useRetries, useRetryTransition } from '@/api/queries';
import { Pill } from '../CommandTable/Pill';
import { RelativeTime } from '../CommandTable/RelativeTime';
import styles from './Settings.module.css';

function PendingMoves() {
  const retries = useRetries();
  const retry = useRetryTransition();
  const dismiss = useDismissRetry();
  const list = retries.data?.retries ?? [];
  if (list.length === 0) return null;
  return (
    <div>
      <h4>Jira moves to retry</h4>
      <ul className={styles.log} aria-label="Jira moves to retry">
        {list.map((r) => (
          <li key={r.id}>
            <div>
              {r.ticketKey} to {r.toStatus}
            </div>
            <div className={styles.error}>{r.lastError}</div>
            <div className={styles.actions}>
              <button
                type="button"
                aria-label={`Retry the move of ${r.ticketKey}`}
                disabled={retry.isPending}
                onClick={() => retry.mutate(r.id)}
              >
                Retry
              </button>
              <button
                type="button"
                aria-label={`Dismiss the move of ${r.ticketKey}`}
                disabled={dismiss.isPending}
                onClick={() => dismiss.mutate(r.id)}
              >
                Dismiss
              </button>
            </div>
          </li>
        ))}
      </ul>
      {(retry.isError || dismiss.isError) && (
        <p className={styles.error} role="alert">
          That did not work. Try again.
        </p>
      )}
    </div>
  );
}

export function ActivitySection() {
  const activity = useActivity(true);
  const items = activity.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section className={styles.section} aria-labelledby="set-activity">
      <h3 id="set-activity">Activity</h3>
      <PendingMoves />
      {activity.isPending && <p className={styles.muted}>Loading activity</p>}
      {activity.isError && (
        <p className={styles.error} role="alert">
          Could not load the activity.
        </p>
      )}
      {activity.isSuccess && items.length === 0 && <p className={styles.muted}>Nothing yet.</p>}
      <ul className={styles.log} aria-label="Activity">
        {items.map((a) => (
          <li key={a.id}>
            <div>
              <Pill tone={a.result === 'ok' ? 'good' : a.result === 'failed' ? 'bad' : 'mute'}>
                {a.result}
              </Pill>{' '}
              {a.summary}
            </div>
            {a.error && <div className={styles.error}>{a.error}</div>}
            <div className={styles.muted}>
              <RelativeTime iso={a.createdAt} /> by {a.triggeredBy}
            </div>
          </li>
        ))}
      </ul>
      {activity.hasNextPage && (
        <button
          type="button"
          disabled={activity.isFetchingNextPage}
          onClick={() => void activity.fetchNextPage()}
        >
          Load more
        </button>
      )}
    </section>
  );
}
