import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { searchTerms } from '@/lib/search';
import { readStored, readStringList, writeStored } from '@/lib/storage';
import { DEFAULT_SORT, type SortKey } from '@/lib/sort';
import type { Filter } from '@/lib/snapshot';
import { isShowMode, type ShowMode } from '@/lib/show';
import { NOTES_KEY, parseNotes, pruneNotes, withNote, type Notes } from '@/lib/notes';
import { DEFAULT_PAGE_SIZE, isHideMode, isPageSize, pruneHidden, type HideMode } from '@/lib/view';

export const HIDDEN_KEY = 'prd-hidden';
export const HIDE_MODE_KEY = 'prd-hidef';
export const PAGE_SIZE_KEY = 'prd-pagesize';
export const SHOW_KEY = 'prd-show';
export const OFF_KEY = 'prd-repos-off';

function storedMode(): HideMode {
  const v = readStored(HIDE_MODE_KEY);
  return isHideMode(v) ? v : 'visible';
}

function storedShow(): ShowMode {
  const v = readStored(SHOW_KEY);
  return isShowMode(v) ? v : 'everything';
}

function storedPageSize(): number {
  const v = Number(readStored(PAGE_SIZE_KEY));
  return isPageSize(v) ? v : DEFAULT_PAGE_SIZE;
}

/**
 * How the list is viewed: filter, search, sort, show, hidden pull requests, page size and page. Everything is
 * client-side. The page goes back to the first whenever what it lists changes.
 * @param existing ids in the snapshot; hidden ids that are not among them are ignored.
 */
export function useListPrefs(existing: readonly string[]) {
  const [filter, setFilterState] = useState<Filter>('all');
  const [query, setQueryState] = useState('');
  const [sort, setSortState] = useState<SortKey>(DEFAULT_SORT);
  const [reversed, setReversed] = useState(false);
  const [mode, setModeState] = useState<HideMode>(storedMode);
  const [show, setShowState] = useState<ShowMode>(storedShow);
  const [pageSize, setPageSizeState] = useState(storedPageSize);
  const [page, setPage] = useState(0);
  const [stored, setStored] = useState(() => new Set(readStringList(HIDDEN_KEY)));
  const [off, setOffState] = useState<ReadonlySet<string>>(() => new Set(readStringList(OFF_KEY)));
  const [storedNotes, setStoredNotes] = useState<Notes>(() => parseNotes(readStored(NOTES_KEY)));

  // Pruned when read, never written back on render. An empty snapshot (still loading) prunes nothing.
  const hidden = useMemo(
    () => (existing.length === 0 ? stored : pruneHidden(stored, existing)),
    [stored, existing],
  );

  // A note whose pull request is gone is dropped when read, and from storage with the next change.
  const notes = useMemo(() => pruneNotes(storedNotes, existing), [storedNotes, existing]);
  const latestNotes = useRef(notes);
  useEffect(() => {
    latestNotes.current = notes;
  }, [notes]);

  const setNote = useCallback((id: string, text: string) => {
    const next = withNote(latestNotes.current, id, text);
    latestNotes.current = next;
    setStoredNotes(next);
    writeStored(NOTES_KEY, JSON.stringify(next));
  }, []);

  // Delayed toggles (the fade-out) must build on the latest set, not the one they were made with.
  const latest = useRef(hidden);
  useEffect(() => {
    latest.current = hidden;
  }, [hidden]);

  const toggleHidden = useCallback((id: string) => {
    const next = new Set(latest.current);
    if (!next.delete(id)) next.add(id);
    latest.current = next;
    setStored(next);
    writeStored(HIDDEN_KEY, JSON.stringify([...next]));
  }, []);

  const terms = useMemo(() => searchTerms(query), [query]);

  return {
    filter,
    query,
    terms,
    sort,
    reversed,
    mode,
    show,
    pageSize,
    page,
    hidden,
    notes,
    setNote,
    off,
    setOff: (next: ReadonlySet<string>) => {
      setOffState(next);
      writeStored(OFF_KEY, JSON.stringify([...next]));
      setPage(0);
    },
    toggleHidden,
    setPage,
    setFilter: (f: Filter) => {
      setFilterState(f);
      setPage(0);
    },
    setQuery: (q: string) => {
      setQueryState(q);
      setPage(0);
    },
    setSort: (s: SortKey) => {
      setSortState(s);
      setPage(0);
    },
    reverse: () => {
      setReversed((r) => !r);
      setPage(0);
    },
    setMode: (m: HideMode) => {
      setModeState(m);
      writeStored(HIDE_MODE_KEY, m);
      setPage(0);
    },
    setShow: (m: ShowMode) => {
      setShowState(m);
      writeStored(SHOW_KEY, m);
      setPage(0);
    },
    setPageSize: (n: number) => {
      setPageSizeState(n);
      writeStored(PAGE_SIZE_KEY, String(n));
      setPage(0);
    },
  };
}

export type ListPrefs = ReturnType<typeof useListPrefs>;
