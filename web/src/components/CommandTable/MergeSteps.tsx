import type { MergeStep } from '@/api/client';
import { useRetryTransition } from '@/api/queries';
import styles from './CommandTable.module.css';
import { Pill, type Tone } from './Pill';

const STATE: Record<MergeStep['state'], { tone: Tone; label: string }> = {
  ok: { tone: 'good', label: 'Done' },
  failed: { tone: 'bad', label: 'Failed' },
  skipped: { tone: 'mute', label: 'Skipped' },
};

function stepName(s: MergeStep): string {
  if (s.kind === 'merge') return 'Merge';
  if (s.kind === 'delete_branch') return 'Delete branch';
  return `Move ${s.ticket ?? 'ticket'}${s.toStatus ? ` to ${s.toStatus}` : ''}`;
}

export function MergeSteps({ steps }: { steps: MergeStep[] }) {
  const retry = useRetryTransition();
  return (
    <ul className={styles.steps} aria-label="Merge steps">
      {steps.map((s, i) => {
        const state = STATE[s.state];
        return (
          <li key={`${s.kind}-${s.ticket ?? ''}-${i}`} className={styles.step}>
            <span>{stepName(s)}</span>
            <Pill tone={state.tone}>{state.label}</Pill>
            {s.message && <span className={styles.muted}>{s.message}</span>}
            {s.state === 'failed' && s.retryId !== null && (
              <button
                type="button"
                className={styles.linkButton}
                aria-label={`Retry ${stepName(s)}`}
                disabled={retry.isPending}
                onClick={() => retry.mutate(s.retryId as number)}
              >
                Retry
              </button>
            )}
          </li>
        );
      })}
      {retry.isError && (
        <li className={styles.errorText} role="alert">
          The retry could not be made.
        </li>
      )}
    </ul>
  );
}
