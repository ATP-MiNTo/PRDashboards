'use client';

import {
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type DragEvent,
} from 'react';
import type { RuleMode, SyncRule } from '@/api/client';
import { useJiraOptions, useRules, useSaveRules, useSnapshot } from '@/api/queries';
import { MAX_RULES, preferList, projectsOf, statusesOf } from '@/lib/rules';
import {
  dropLineY,
  dropSlot,
  reorder,
  RULE_TEMPLATES,
  ruleFromTemplate,
  sameRule,
} from '@/lib/rulesEditor';
import { Modal } from '../Modal/Modal';
import { ModalHead } from '../Modal/ModalHead';
import { MenuItem, PopMenu } from '../PopMenu/PopMenu';
import { ActivityList } from './ActivityList';
import { ExcludeStatuses } from './ExcludeStatuses';
import { PreviewPanel } from './PreviewPanel';
import { RuleCard, type CardDrag } from './RuleCard';
import styles from './RulesModal.module.css';

const TIP =
  'Rules run top to bottom and the first one that holds for a ticket moves it. Empty means Any. Auto moves, Off skips. Drag the grip to reorder.';

/** The scroll area shows this many cards at once; more are scrolled. */
const VISIBLE_CARDS = 2;

const repoNames = (s: { prs: { repo: { nameWithOwner: string } }[] } | undefined) =>
  [...new Set((s?.prs ?? []).map((p) => p.repo.nameWithOwner))].sort((a, b) => a.localeCompare(b));
const baseNames = (s: { prs: { baseRef: string }[] } | undefined) =>
  [...new Set((s?.prs ?? []).map((p) => p.baseRef))].sort((a, b) => a.localeCompare(b));

/**
 * The status sync rules dialog: the ordered rule cards (drag the grip, or use the keys, to reorder),
 * the "Add rule" menu of templates, the Preview, the Exclude statuses and the Activity. Reorder,
 * disable, delete and the mode of shared tickets save at once, as they always did. Any other edit
 * stays in the card until its "Save rule", which asks for a preview first.
 */
export function RulesModal({ onClose }: { onClose: () => void }) {
  const titleId = useId();
  const rules = useRules();
  const save = useSaveRules();
  // Jira statuses and projects cost a server-side Jira call, so they load when the dialog opens,
  // never on mount of the page.
  const options = useJiraOptions(true);
  const snapshotStatuses = useSnapshot(statusesOf).data;
  const snapshotProjects = useSnapshot(projectsOf).data;
  const repos = useSnapshot(repoNames).data;
  const branches = useSnapshot(baseNames).data;
  const statuses = useMemo(
    () => preferList(options.data?.statuses, snapshotStatuses ?? []),
    [options.data, snapshotStatuses],
  );
  const projects = useMemo(
    () => preferList(options.data?.projects, snapshotProjects ?? []),
    [options.data, snapshotProjects],
  );

  const list = useMemo(() => rules.data?.rules ?? [], [rules.data]);
  // What the cards show on top of the saved rules, and the ids of the rules that are not saved yet.
  const [edits, setEdits] = useState<Record<string, SyncRule>>({});
  const [fresh, setFresh] = useState<string[]>([]);
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const [showPreview, setShowPreview] = useState(false);
  // What gets the focus once the next render has put it on the page.
  const focusNext = useRef<{ id: string; what: 'name' | 'up' | 'down' } | null>(null);

  const shown = useMemo(
    () => [
      ...list.map((r) => edits[r.id] ?? r),
      ...fresh.flatMap((id) => (edits[id] ? [edits[id]] : [])),
    ],
    [list, edits, fresh],
  );
  const savedOf = (id: string) => list.find((r) => r.id === id) ?? null;
  const isPending = (r: SyncRule) => {
    const s = savedOf(r.id);
    return s === null || !sameRule(s, r);
  };
  const unsaved = shown.some(isPending);

  const forget = useCallback((id: string) => {
    setEdits((e) => {
      const rest = { ...e };
      delete rest[id];
      return rest;
    });
    setFresh((f) => f.filter((x) => x !== id));
  }, []);

  const put = (next: readonly SyncRule[], baseline = false) =>
    save.mutate({ rules: [...next], baseline });
  const edit = (r: SyncRule, patch: Partial<SyncRule>) =>
    setEdits((e) => ({ ...e, [r.id]: { ...(e[r.id] ?? r), ...patch } }));
  const setEnabled = (r: SyncRule, enabled: boolean) => {
    // Enabling a rule evaluates it against the current tickets, so it waits for a preview.
    if (enabled || isPending(r)) edit(r, { enabled });
    else put(list.map((x) => (x.id === r.id ? { ...x, enabled } : x)));
  };
  const setMode = (r: SyncRule, mode: RuleMode) => {
    if (r.mode === mode) return;
    // A mode alone saves at once with the baseline, so nothing moves because of it.
    if (!isPending(r))
      put(
        list.map((x) => (x.id === r.id ? { ...x, mode } : x)),
        true,
      );
    else edit(r, { mode });
  };
  const remove = (r: SyncRule) => {
    if (savedOf(r.id)) put(list.filter((x) => x.id !== r.id));
    forget(r.id);
  };
  const move = (from: number, slot: number) => {
    const next = reorder(list, from, slot);
    if (next !== list) put(next);
  };

  const toggleFold = (id: string) =>
    setOpen((o) => {
      const next = new Set(o);
      if (!next.delete(id)) next.add(id);
      return next;
    });

  const add = (template: (typeof RULE_TEMPLATES)[number]) => {
    const r = ruleFromTemplate(template, statuses);
    setEdits((e) => ({ ...e, [r.id]: r }));
    setFresh((f) => [...f, r.id]);
    setOpen((o) => new Set(o).add(r.id));
    focusNext.current = { id: r.id, what: 'name' };
  };

  // A new rule scrolls into view with the cursor in its name, and a card moved with its Up or Down
  // button keeps that button focused (the card is a new element once it has changed places).
  const area = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const next = focusNext.current;
    if (!next) return;
    focusNext.current = null;
    const card = area.current?.querySelector<HTMLElement>(
      `[data-rule-card="${CSS.escape(next.id)}"]`,
    );
    if (next.what === 'name') {
      card?.scrollIntoView?.({ block: 'nearest' });
      card?.querySelector<HTMLInputElement>('input[type="text"]')?.select();
    } else {
      const keys = card?.querySelectorAll<HTMLButtonElement>('button[aria-label^="Move "]');
      (next.what === 'up' ? keys?.[0] : keys?.[1])?.focus();
    }
  }, [list, shown]);

  // Three or more cards: the list shows two of them at their full height and scrolls the rest.
  const scroller = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const cards = [...el.querySelectorAll<HTMLElement>('[data-rule-card]')];
    if (cards.length <= VISIBLE_CARDS) {
      el.style.maxHeight = '';
      return;
    }
    const full = (c: HTMLElement) =>
      26 +
      (c.querySelector<HTMLElement>('[data-rule-head]')?.offsetHeight ?? 0) +
      (c.querySelector<HTMLElement>('[data-rule-body]')?.scrollHeight ?? 0);
    const height = cards.slice(0, VISIBLE_CARDS).reduce((sum, c) => sum + full(c), 12);
    el.style.maxHeight = height > 12 ? `${height}px` : '';
  });

  // ----- dragging a card by its grip, with a line where it will land -----
  const from = useRef<number | null>(null);
  const at = useRef<number | null>(null);
  const line = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState<string | null>(null);
  const cardRects = () =>
    [...(scroller.current?.querySelectorAll<HTMLElement>('[data-rule-card]') ?? [])]
      .slice(0, list.length)
      .map((c) => c.getBoundingClientRect());
  const stopDrag = useCallback(() => {
    from.current = null;
    at.current = null;
    if (line.current) line.current.hidden = true;
    setDragging(null);
    scroller.current
      ?.querySelectorAll<HTMLElement>('[data-rule-card]')
      .forEach((c) => (c.draggable = false));
  }, []);
  const drag: CardDrag = {
    onGrip: (card) => {
      card.draggable = true;
    },
    onStart: (e: DragEvent<HTMLElement>, index) => {
      from.current = index;
      if (e.dataTransfer) {
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', String(index));
      }
      const id = list[index]?.id ?? null;
      // The dim look comes a frame later, so the drag image is the card as it was.
      requestAnimationFrame(() => from.current !== null && setDragging(id));
    },
    onEnd: stopDrag,
  };
  const onDragOver = (e: DragEvent<HTMLElement>) => {
    const el = scroller.current;
    if (from.current === null || !el) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    const rects = cardRects();
    const slot = dropSlot(e.clientY, rects);
    at.current = slot;
    const box = el.getBoundingClientRect();
    if (e.clientY < box.top + 36) el.scrollTop -= 14;
    else if (e.clientY > box.bottom - 36) el.scrollTop += 14;
    if (line.current) {
      line.current.style.top = `${dropLineY(slot, rects) - box.top + el.scrollTop - 1.5}px`;
      line.current.hidden = false;
    }
  };
  const onDrop = (e: DragEvent<HTMLElement>) => {
    if (from.current === null || at.current === null) return;
    e.preventDefault();
    const [f, slot] = [from.current, at.current];
    stopDrag();
    move(f, slot);
  };

  return (
    <Modal labelledBy={titleId} onClose={onClose} wide>
      <ModalHead titleId={titleId} title="Status sync rules" tip={TIP} onClose={onClose} />
      <div ref={area}>
        {rules.isPending && <p className={styles.muted}>Loading rules</p>}
        {rules.isError && (
          <p className={styles.error} role="alert">
            Could not load the rules.
          </p>
        )}
        {rules.isSuccess && shown.length === 0 && <p className={styles.muted}>No rules yet.</p>}
        <div
          ref={scroller}
          className={shown.length > VISIBLE_CARDS ? `${styles.list} ${styles.scroll}` : styles.list}
          onDragOver={onDragOver}
          onDrop={onDrop}
        >
          <ol className={styles.cards} aria-label="Status sync rules">
            {shown.map((r, i) => (
              <RuleCard
                key={r.id}
                index={i}
                rule={r}
                saved={savedOf(r.id)}
                others={list}
                collapsed={!open.has(r.id)}
                dragging={dragging === r.id}
                first={i === 0}
                last={i === list.length - 1}
                busy={save.isPending}
                statuses={statuses}
                projects={projects}
                repos={repos ?? []}
                branches={branches ?? []}
                drag={drag}
                onEdit={(patch) => edit(r, patch)}
                onEnabled={(on) => setEnabled(r, on)}
                onMode={(mode) => setMode(r, mode)}
                onFold={() => toggleFold(r.id)}
                onDelete={() => remove(r)}
                onMove={(dir) => {
                  focusNext.current = { id: r.id, what: dir === 1 ? 'down' : 'up' };
                  move(i, dir === 1 ? i + 2 : i - 1);
                }}
                onSaved={() => forget(r.id)}
                onDiscard={() => forget(r.id)}
              />
            ))}
          </ol>
          <div ref={line} className={styles.dropLine} hidden />
        </div>
      </div>
      {save.isError && (
        <p className={styles.error} role="alert">
          Could not save the rules.
        </p>
      )}
      <div className={styles.addRow}>
        <PopMenu
          label="Add rule"
          trigger="Add rule"
          triggerClassName={styles.primary}
          menuLabel="Rule templates"
          width="w72"
          side="above"
          disabled={shown.length >= MAX_RULES || !rules.isSuccess}
        >
          {(close) =>
            RULE_TEMPLATES.map((t) => (
              <MenuItem
                key={t.label}
                onSelect={() => {
                  add(t);
                  close();
                }}
              >
                <span className={styles.wrap}>{t.label}</span>
              </MenuItem>
            ))
          }
        </PopMenu>
        <button
          type="button"
          className={styles.outline}
          aria-expanded={showPreview}
          onClick={() => setShowPreview((v) => !v)}
        >
          Preview
        </button>
      </div>
      {showPreview && <PreviewPanel rules={shown} unsaved={unsaved} />}
      <ExcludeStatuses statuses={statuses} />
      <ActivityList />
    </Modal>
  );
}
