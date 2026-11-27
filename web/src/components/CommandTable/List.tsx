import { useCallback, useEffect, useRef, useState } from 'react';
import type { Snapshot } from '@/api/client';
import { useSnapshot } from '@/api/queries';
import { groupsOf, type Group } from '@/lib/snapshot';
import { sortGroups } from '@/lib/sort';
import { reducedMotion } from '@/lib/storage';
import { applyHidden, paginate, shownIn } from '@/lib/view';
import { Pager } from '../Toolbar/Pager';
import type { ListPrefs } from '../Toolbar/useListPrefs';
import styles from './CommandTable.module.css';
import { Pill } from './Pill';
import { Row } from './Row';

const COLUMNS = ['Repo / PR', 'Ticket', 'Last commit', 'Review', 'CI', 'Method', 'Merge'] as const;

/** How long a row fades out before it leaves the list; matches the ghost-out animation. */
export const GHOST_MS = 500;

export function Table({ prefs }: { prefs: ListPrefs }) {
  const { filter, terms, sort, reversed, mode, show, hidden, toggleHidden, pageSize, page } = prefs;
  const select = useCallback(
    (s: Snapshot) => {
      const byId = new Map(s.prs.map((p) => [p.id, p]));
      const groups = sortGroups(groupsOf(s, filter, Date.now(), show, terms), byId, sort, reversed);
      return applyHidden(groups, hidden, mode);
    },
    [filter, terms, sort, reversed, show, hidden, mode],
  );
  const groups = useSnapshot<Group[]>(select).data ?? [];
  const pages = paginate(groups, pageSize);
  const current = Math.min(page, Math.max(0, pages.length - 1));

  // Rows that are about to leave the list fade out first; the toggle is applied when it ends.
  const [leaving, setLeaving] = useState<ReadonlySet<string>>(new Set());
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  useEffect(() => {
    const all = timers.current;
    return () => all.forEach(clearTimeout);
  }, []);

  const hide = useCallback(
    (id: string) => {
      const isHidden = hidden.has(id);
      if (reducedMotion() || leaving.has(id) || shownIn(mode, !isHidden)) {
        toggleHidden(id);
        return;
      }
      setLeaving((s) => new Set(s).add(id));
      const t = setTimeout(() => {
        timers.current.delete(t);
        toggleHidden(id);
        setLeaving((s) => {
          const next = new Set(s);
          next.delete(id);
          return next;
        });
      }, GHOST_MS);
      timers.current.add(t);
    },
    [hidden, leaving, mode, toggleHidden],
  );

  const empty =
    terms.length > 0
      ? 'No pull requests match the search.'
      : mode === 'hidden'
        ? 'No hidden pull requests match.'
        : hidden.size > 0 && mode === 'visible'
          ? 'No pull requests match. Some are hidden.'
          : 'No pull requests match.';

  return (
    <>
      <div className={styles.tableWrap}>
        <table className={styles.table}>
          <thead>
            <tr>
              {COLUMNS.map((c) => (
                <th key={c} scope="col">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          {(pages[current] ?? []).map((g) => (
            <tbody key={g.key} className={g.stacked ? styles.stackGroup : undefined}>
              {g.stacked && (
                <tr className={styles.stackHeader}>
                  <td colSpan={COLUMNS.length}>
                    <Pill tone="stack">Stack of {g.ids.length}</Pill>{' '}
                    <span className={styles.muted}>merge from the bottom up</span>
                  </td>
                </tr>
              )}
              {g.ids.map((id) => (
                <Row
                  key={id}
                  id={id}
                  hidden={hidden.has(id) !== leaving.has(id)}
                  dim={mode === 'all' && hidden.has(id) && !leaving.has(id)}
                  leaving={leaving.has(id)}
                  onHide={hide}
                />
              ))}
            </tbody>
          ))}
        </table>
        {groups.length === 0 && <p className={styles.empty}>{empty}</p>}
      </div>
      <Pager
        pages={pages}
        page={current}
        pageSize={pageSize}
        onPage={prefs.setPage}
        onPageSize={prefs.setPageSize}
      />
    </>
  );
}
