import type { RulePreview } from '@/api/client';
import { refText } from '@/lib/rules';
import styles from './RulesModal.module.css';

/** What one rule would do now, shown in the rule's card before it is saved. */
export function PreviewResult({ preview }: { preview: RulePreview }) {
  const moves = preview.moves.filter((m) => !m.alreadyInTarget);
  const there = preview.moves.filter((m) => m.alreadyInTarget);
  return (
    <div className={styles.result} role="region" aria-label="Preview">
      {moves.length === 0 ? (
        <p>No ticket would move now.</p>
      ) : (
        <>
          <p>
            {moves.length === 1
              ? '1 ticket would move now:'
              : `${moves.length} tickets would move now:`}
          </p>
          <ul>
            {moves.map((m) => (
              <li key={m.ticketKey}>
                {m.ticketKey} ({m.status})
              </li>
            ))}
          </ul>
        </>
      )}
      {there.length > 0 && (
        <p className={styles.muted}>
          Already in the target status: {there.map((m) => m.ticketKey).join(', ')}.
        </p>
      )}
      {preview.waiting.length > 0 && (
        <ul aria-label="Waiting tickets">
          {preview.waiting.map((w) => (
            <li key={w.ticketKey} className={styles.muted}>
              {w.ticketKey}: waiting on {w.waitingOn.length} of {w.total} PRs:{' '}
              {w.waitingOn.map(refText).join(', ')}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
