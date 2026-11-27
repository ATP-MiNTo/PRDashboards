import { useEffect, useMemo, useRef, useState } from 'react';
import type { SyncRule } from '@/api/client';
import { usePreviewRules, useSnapshot } from '@/api/queries';
import { mergeTable, previewKey, previewTable, type PreviewTable } from '@/lib/rulesEditor';
import { ruleProblem } from '@/lib/rules';
import styles from './RulesModal.module.css';

/** The time the rules have to rest before the preview asks the server again. */
export const PREVIEW_DELAY_MS = 250;

type When = 'now' | 'merged';

/**
 * The Preview panel: "Right now" is what the enabled rules, as they stand in the dialog, would move
 * with the cached data (one batch request for all rules, answered from the database), and "If merged"
 * is what the saved merged rules do once the pull requests merge (from the merge plans of the
 * snapshot). Tickets no rule moves are listed under "Won't move".
 */
export function PreviewPanel({ rules, unsaved }: { rules: SyncRule[]; unsaved: boolean }) {
  const [when, setWhen] = useState<When>('now');
  const preview = usePreviewRules();
  const snapshot = useSnapshot().data;
  const { mutate } = preview;

  const usable = useMemo(() => rules.filter((r) => r.enabled && ruleProblem(r) === null), [rules]);
  const key = usable.map((r) => `${r.id}:${previewKey(r)}`).join('|');
  const latest = useRef(usable);
  useEffect(() => {
    latest.current = usable;
  });
  const none = usable.length === 0;

  useEffect(() => {
    if (when !== 'now' || none) return;
    const timer = setTimeout(() => mutate(latest.current), PREVIEW_DELAY_MS);
    return () => clearTimeout(timer);
  }, [when, key, none, mutate]);

  const names = useMemo(
    () => new Map(rules.map((r) => [r.id, r]) as [string, SyncRule][]),
    [rules],
  );
  const table: PreviewTable | null = useMemo(() => {
    if (when === 'merged') return mergeTable(snapshot);
    if (none) return { rows: [], stays: [] };
    if (!preview.data) return null;
    // The names are the ones typed now; the answers are for the rules as they were asked.
    return previewTable(
      preview.data.map(({ rule, preview: p }) => ({
        rule: names.get(rule.id) ?? rule,
        preview: p,
      })),
    );
  }, [when, none, preview.data, snapshot, names]);

  return (
    <section className={styles.preview} aria-label="Preview of all rules">
      <div className={styles.previewHead}>
        <div className={styles.eyebrow}>Preview</div>
        <div className={styles.join} role="group" aria-label="Preview when">
          <button
            type="button"
            className={styles.joinBtn}
            aria-pressed={when === 'now'}
            onClick={() => setWhen('now')}
          >
            Right now
          </button>
          <button
            type="button"
            className={styles.joinBtn}
            aria-pressed={when === 'merged'}
            onClick={() => setWhen('merged')}
          >
            If merged
          </button>
        </div>
      </div>
      {when === 'merged' && unsaved && (
        <p className={styles.muted}>
          This is what the saved rules do. Save your changes to include them.
        </p>
      )}
      {preview.isError && when === 'now' && (
        <p className={styles.error} role="alert">
          Could not preview the rules.
        </p>
      )}
      {table === null ? (
        !preview.isError && <p className={styles.muted}>Loading the preview</p>
      ) : (
        <>
          {table.rows.length > 0 ? (
            <div className={styles.tableWrap}>
              <table className={styles.table}>
                <thead>
                  <tr>
                    <th scope="col">Ticket</th>
                    <th scope="col">When</th>
                    <th scope="col">Moves</th>
                    <th scope="col">Because</th>
                  </tr>
                </thead>
                <tbody>
                  {table.rows.map((r) => (
                    <tr key={r.ticket}>
                      <td className={styles.strong}>{r.ticket}</td>
                      <td>
                        <span className={styles.badgeOutline}>{r.when}</span>
                      </td>
                      <td className={styles.nowrap}>
                        <span className={styles.badgeGhost}>{r.from}</span> →{' '}
                        <span className={styles.badgeInfo}>{r.to}</span>
                      </td>
                      <td>
                        <span className={styles.badgeOutline}>{r.because}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className={styles.plain}>
              No ticket would move{when === 'merged' ? ' if these PRs merged' : ' right now'}.
            </p>
          )}
          {table.stays.length > 0 && (
            <details className={styles.stays}>
              <summary>Won&apos;t move ({table.stays.length})</summary>
              <ul>
                {table.stays.map((s) => (
                  <li key={s.ticket}>
                    <b>{s.ticket}</b>
                    {s.from ? <span className={styles.faint}> ({s.from})</span> : null}: {s.why}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </>
      )}
    </section>
  );
}
