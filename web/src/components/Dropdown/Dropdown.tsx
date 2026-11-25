'use client';

import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import styles from './Dropdown.module.css';
import { usePopover } from './usePopover';

export interface DropdownOption<T extends string> {
  value: T;
  label: string;
  disabled?: boolean;
}

export interface DropdownProps<T extends string> {
  /** The accessible name of the control. */
  label: string;
  value: T;
  options: readonly DropdownOption<T>[];
  onChange: (value: T) => void;
  disabled?: boolean;
  /** Milliseconds the pointer must rest before the list opens on hover. */
  openDelay?: number;
  /** Milliseconds the pointer may be away before the list closes. */
  closeDelay?: number;
}

const TYPEAHEAD_RESET_MS = 600;
/** Lists longer than this get a search box. */
const SEARCH_MIN = 12;

/** The time, read only from event handlers. */
const clock = () => Date.now();

/** Ref of the active option: keeps it visible in the scrolling list (jsdom has no scrollIntoView). */
const reveal = (el: HTMLLIElement | null) => el?.scrollIntoView?.({ block: 'nearest' });

export function Dropdown<T extends string>({
  label,
  value,
  options,
  onChange,
  disabled = false,
  openDelay = 150,
  closeDelay = 250,
}: DropdownProps<T>) {
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const input = useRef<HTMLInputElement>(null);
  const typed = useRef({ text: '', at: 0 });
  const pinned = useRef(false);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);
  const [query, setQuery] = useState('');

  const searchable = options.length > SEARCH_MIN;
  const needle = query.trim().toLowerCase();
  const matches = (o: DropdownOption<T>) => !needle || o.label.toLowerCase().includes(needle);
  const selected = options.findIndex((o) => o.value === value);
  const enabled = options.flatMap((o, i) => (o.disabled || !matches(o) ? [] : [i]));

  const clear = () => clearTimeout(timer.current);
  useEffect(() => () => clearTimeout(timer.current), []);

  const { pop, place } = usePopover(open, button, setOpen);

  const later = (fn: () => void, ms: number) => {
    clear();
    timer.current = setTimeout(fn, ms);
  };

  const show = (byHover = false) => {
    pinned.current = !byHover;
    setQuery('');
    setActive(selected >= 0 && !options[selected]?.disabled ? selected : (enabled[0] ?? -1));
    setOpen(true);
  };

  const hide = () => {
    clear();
    setOpen(false);
  };

  // A blur only closes the list when focus left both the button and the popover.
  const blurred = () =>
    later(() => {
      const at = document.activeElement;
      if (at !== button.current && !pop.current?.contains(at)) setOpen(false);
    }, 0);

  const leave = () => {
    if (!pinned.current) later(() => setOpen(false), closeDelay);
  };

  const onClick = () => {
    clear();
    if (!open) show();
    else if (pinned.current) hide();
    else pinned.current = true;
  };

  // A list opened by click or key puts the cursor in its search box.
  useEffect(() => {
    if (open && pinned.current) input.current?.focus();
  }, [open]);

  const move = (to: number) => {
    if (enabled.length > 0) setActive(to);
  };
  const step = (dir: 1 | -1) => {
    if (enabled.length === 0) return;
    const at = enabled.indexOf(active);
    const next = at < 0 ? (dir === 1 ? 0 : enabled.length - 1) : at + dir;
    move(enabled[Math.min(Math.max(next, 0), enabled.length - 1)] ?? -1);
  };

  const choose = (i: number) => {
    const o = options[i];
    if (!o || o.disabled) return;
    hide();
    if (o.value !== value) onChange(o.value);
    button.current?.focus();
  };

  const typeahead = (char: string) => {
    const now = clock();
    const t = typed.current;
    t.text = now - t.at > TYPEAHEAD_RESET_MS ? char : t.text + char;
    t.at = now;
    const text = t.text.toLowerCase();
    const from =
      t.text.length === 1 ? enabled.indexOf(active) + 1 : Math.max(enabled.indexOf(active), 0);
    const order = [...enabled.slice(from), ...enabled.slice(0, from)];
    const hit = order.find((i) => options[i]?.label.toLowerCase().startsWith(text));
    if (hit === undefined) return;
    if (!open) show();
    setActive(hit);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLElement>, search: boolean) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        e.preventDefault();
        if (!open) show();
        else step(e.key === 'ArrowDown' ? 1 : -1);
        return;
      case 'Home':
      case 'End':
        if (search) return;
        e.preventDefault();
        if (!open) show();
        move((e.key === 'Home' ? enabled[0] : enabled[enabled.length - 1]) ?? -1);
        return;
      case 'Enter':
      case ' ':
        if (search && e.key === ' ') return;
        e.preventDefault();
        if (open) choose(active);
        else show();
        return;
      case 'Escape':
        if (open) {
          e.preventDefault();
          e.stopPropagation();
          hide();
          button.current?.focus();
        }
        return;
      case 'Tab':
        hide();
        // Focus the button first so the default Tab moves on from it, not from the portal.
        if (search) button.current?.focus();
        return;
    }
    if (!search && e.key.length === 1) {
      e.preventDefault();
      typeahead(e.key);
    }
  };

  const onSearchKey = (e: KeyboardEvent<HTMLInputElement>) => onKeyDown(e, true);

  return (
    <span
      className={styles.root}
      onMouseEnter={() => !disabled && !open && later(() => show(true), openDelay)}
      onMouseLeave={leave}
    >
      <button
        ref={button}
        type="button"
        role="combobox"
        className={styles.button}
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-activedescendant={open && active >= 0 ? `${id}-o${active}` : undefined}
        disabled={disabled}
        onClick={onClick}
        onKeyDown={(e) => onKeyDown(e, false)}
        onBlur={blurred}
      >
        <span>{options[selected]?.label ?? ''}</span>
        <span aria-hidden="true" className={styles.caret} />
      </button>
      {open &&
        createPortal(
          <div
            ref={place}
            className={styles.popover}
            onMouseEnter={clear}
            onMouseLeave={leave}
            onMouseDown={(e) => {
              // Keep the focus where it is, except on the search box that needs it.
              if (e.target !== input.current) e.preventDefault();
            }}
          >
            {searchable && (
              <input
                ref={input}
                type="search"
                className={styles.search}
                aria-label={`Search ${label}`}
                aria-controls={id}
                aria-activedescendant={active >= 0 ? `${id}-o${active}` : undefined}
                autoComplete="off"
                value={query}
                onChange={(e) => {
                  const text = e.target.value;
                  const lower = text.trim().toLowerCase();
                  setQuery(text);
                  setActive(
                    options.findIndex(
                      (o) => !o.disabled && (!lower || o.label.toLowerCase().includes(lower)),
                    ),
                  );
                }}
                onKeyDown={onSearchKey}
                onBlur={blurred}
              />
            )}
            <ul id={id} role="listbox" aria-label={label} className={styles.list}>
              {options.map((o, i) =>
                matches(o) ? (
                  <li
                    key={o.value}
                    id={`${id}-o${i}`}
                    ref={i === active ? reveal : undefined}
                    role="option"
                    aria-selected={i === selected}
                    aria-disabled={o.disabled || undefined}
                    className={[
                      styles.option,
                      i === active ? styles.active : '',
                      o.disabled ? styles.disabled : '',
                    ].join(' ')}
                    onMouseMove={() => !o.disabled && active !== i && setActive(i)}
                    onClick={() => choose(i)}
                  >
                    {o.label}
                  </li>
                ) : null,
              )}
            </ul>
            {enabled.length === 0 && needle && <p className={styles.empty}>No matches</p>}
          </div>,
          document.body,
        )}
    </span>
  );
}
