import { useActivity, useDismissRetry, useRetries, useRetryTransition } from '@/api/queries';
import { Pill, type Tone } from '../CommandTable/Pill';
import { RelativeTime } from '../CommandTable/RelativeTime';
import styles from './RulesModal.module.css';

const RESULT_TONE: Record<string, Tone> = { ok: 'good', failed: 'bad', needs_input: 'warn' };
const RESULT_LABEL: Record<string, string> = { needs_input: 'needs input' };

function byText(by: string): string {
  return by.startsWith('rule:') ? 'a status sync rule' : by;
}

function PendingMoves() {
  const retries = useRetries();
  const retry = useRetryTransition();
  const dismiss = useDismissRetry();
  const list = retries.data?.retries ?? [];
  if (list.length === 0) return null;
  return (
    <div className={styles.retries}>
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
                className={styles.outlineXs}
                aria-label={`Retry the move of ${r.ticketKey}`}
                disabled={retry.isPending}
                onClick={() => retry.mutate(r.id)}
              >
                Retry
              </button>
              <button
                type="button"
                className={styles.outlineXs}
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

/** The moves the rules made, newest first, and the Jira moves that failed and wait for a retry. */
export function ActivityList() {
  const activity = useActivity(true);
  const items = activity.data?.pages.flatMap((p) => p.items) ?? [];
  return (
    <section className={styles.section} aria-labelledby="rules-activity">
      <div id="rules-activity" className={styles.eyebrow}>
        Activity
      </div>
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
              <Pill tone={RESULT_TONE[a.result] ?? 'mute'}>
                {RESULT_LABEL[a.result] ?? a.result}
              </Pill>{' '}
              {a.summary}
            </div>
            {a.result === 'needs_input' && (
              <div className={styles.muted}>
                Jira asks for fields on this move. Open the ticket in Jira and move it there.
              </div>
            )}
            {a.error && <div className={styles.error}>{a.error}</div>}
            <div className={styles.muted}>
              <RelativeTime iso={a.createdAt} /> by {byText(a.triggeredBy)}
            </div>
          </li>
        ))}
      </ul>
      {activity.hasNextPage && (
        <div className={styles.actions}>
          <button
            type="button"
            className={styles.outlineXs}
            disabled={activity.isFetchingNextPage}
            onClick={() => void activity.fetchNextPage()}
          >
            Load more
          </button>
        </div>
      )}
    </section>
  );
}
