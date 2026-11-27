import type { Snapshot } from '@/api/client';
import type { ShowMode } from './show';
import { blockOf, groupsOf, type Filter, type Group } from './snapshot';

/** Which pull requests the list shows: visible only, all with hidden ones dimmed, hidden only. */
export type HideMode = 'visible' | 'all' | 'hidden';

export const HIDE_MODES: HideMode[] = ['visible', 'all', 'hidden'];

export const PAGE_SIZES = [10, 15, 20] as const;
export const DEFAULT_PAGE_SIZE = 10;

export function isHideMode(v: unknown): v is HideMode {
  return v === 'visible' || v === 'all' || v === 'hidden';
}

export function isPageSize(v: unknown): v is number {
  return typeof v === 'number' && (PAGE_SIZES as readonly number[]).includes(v);
}

/** The ids that are both hidden and still in the snapshot. */
export function pruneHidden(hidden: ReadonlySet<string>, existing: Iterable<string>): Set<string> {
  const out = new Set<string>();
  for (const id of existing) if (hidden.has(id)) out.add(id);
  return out;
}

/** Whether a row shows in a mode. */
export function shownIn(mode: HideMode, isHidden: boolean): boolean {
  return mode === 'all' || (mode === 'hidden') === isHidden;
}

/** Drops the ids a mode does not show, and the groups left empty. Members keep their order. */
export function applyHidden(groups: Group[], hidden: ReadonlySet<string>, mode: HideMode): Group[] {
  if (mode === 'all') return groups;
  const out: Group[] = [];
  for (const g of groups) {
    const ids = g.ids.filter((id) => shownIn(mode, hidden.has(id)));
    if (ids.length === g.ids.length) out.push(g);
    else if (ids.length > 0) out.push({ ...g, ids });
  }
  return out;
}

/**
 * Splits groups into pages. A page is closed once it holds `size` pull requests or more, so a
 * stack is never split and a page can run over its size.
 */
export function paginate(groups: Group[], size: number): Group[][] {
  const pages: Group[][] = [];
  let page: Group[] = [];
  let count = 0;
  for (const g of groups) {
    page.push(g);
    count += g.ids.length;
    if (count >= size) {
      pages.push(page);
      page = [];
      count = 0;
    }
  }
  if (page.length > 0) pages.push(page);
  return pages;
}

export interface PageRange {
  /** 1-based position of the first pull request on the page. */
  from: number;
  to: number;
  total: number;
}

/** "Showing from to to of total" for a page index. */
export function pageRange(pages: Group[][], index: number): PageRange {
  const count = (p: Group[]) => p.reduce((n, g) => n + g.ids.length, 0);
  const before = pages.slice(0, index).reduce((n, p) => n + count(p), 0);
  const here = pages[index] ? count(pages[index]) : 0;
  const total = pages.reduce((n, p) => n + count(p), 0);
  return { from: here === 0 ? 0 : before + 1, to: before + here, total };
}

/** Page numbers to show, with null where pages are skipped: first, last and the current ±1. */
export function pageItems(current: number, count: number): (number | null)[] {
  const keep = new Set([0, count - 1, current - 1, current, current + 1]);
  const out: (number | null)[] = [];
  let last = -1;
  for (let i = 0; i < count; i++) {
    if (!keep.has(i)) continue;
    if (i - last === 2) out.push(i - 1);
    else if (i - last > 2) out.push(null);
    out.push(i);
    last = i;
  }
  return out;
}

/** The numbers of the stat tiles. */
export interface ListStats {
  /** Pull requests in the list, with the tile filter applied. */
  showing: number;
  /** The three below ignore the tile filter, so a tile keeps its number while it is the filter. */
  ready: number;
  running: number;
  /** Stacks, not the pull requests in them. */
  stacks: number;
}

export interface ListView {
  filter: Filter;
  show: ShowMode;
  terms: readonly string[];
  hidden: ReadonlySet<string>;
  mode: HideMode;
  /** Ids of the repositories switched off in the sidebar. */
  off: ReadonlySet<string>;
}

const count = (groups: Group[]) => groups.reduce((n, g) => n + g.ids.length, 0);

/** What the stat tiles say about the list as the Show, hide and search switches leave it. */
export function listStats(s: Snapshot, v: ListView): ListStats {
  const base = applyHidden(groupsOf(s, 'all', v.show, v.terms, v.off), v.hidden, v.mode);
  const byId = new Map(s.prs.map((p) => [p.id, p]));
  const out: ListStats = { showing: 0, ready: 0, running: 0, stacks: 0 };
  for (const g of base) {
    if (g.stacked) out.stacks += 1;
    for (const id of g.ids) {
      const p = byId.get(id);
      if (!p) continue;
      if (blockOf(p).readiness === 'ready') out.ready += 1;
      if (p.ci.state === 'running') out.running += 1;
    }
  }
  out.showing =
    v.filter === 'all'
      ? count(base)
      : count(applyHidden(groupsOf(s, v.filter, v.show, v.terms, v.off), v.hidden, v.mode));
  return out;
}

/** Where a merged card was in the list when its launch began, so it can stay there while it plays. */
export interface Launched {
  id: string;
  /** The key of the group the card was in, and whether that group is a stack. */
  groupKey: string;
  stacked: boolean;
  /** The group's position in the list, and the card's position in the group. */
  groupIndex: number;
  index: number;
}

/** Where `id` is in `groups`, as the facts a `Launched` needs. */
export function locate(groups: readonly Group[], id: string): Omit<Launched, 'id'> | null {
  for (const [groupIndex, g] of groups.entries()) {
    const index = g.ids.indexOf(id);
    if (index >= 0) return { groupKey: g.key, stacked: g.stacked, groupIndex, index };
  }
  return null;
}

/**
 * Puts launched cards back into the list after the snapshot dropped them, so the launch animation
 * can finish before they leave. A card the snapshot still has stays where the list put it. One that
 * was in a stack goes back into that stack while the stack is still listed, else alone at its old
 * position.
 */
export function pinLaunched(
  groups: Group[],
  launched: readonly Launched[],
  present: ReadonlySet<string>,
): Group[] {
  if (!launched.some((l) => !present.has(l.id))) return groups;
  const out = groups.map((g) => ({ ...g, ids: [...g.ids] }));
  for (const l of launched) {
    if (present.has(l.id)) continue;
    const home = l.stacked ? out.find((g) => g.stacked && g.key === l.groupKey) : undefined;
    if (home) {
      home.ids.splice(Math.min(l.index, home.ids.length), 0, l.id);
    } else {
      out.splice(Math.min(l.groupIndex, out.length), 0, { key: l.id, ids: [l.id], stacked: false });
    }
  }
  return out;
}
