'use client';

import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import styles from './Dropdown.module.css';

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
  const typed = useRef({ text: '', at: 0 });
  const pinned = useRef(false);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(-1);

  const selected = options.findIndex((o) => o.value === value);
  const enabled = options.flatMap((o, i) => (o.disabled ? [] : [i]));

  const clear = () => clearTimeout(timer.current);
  useEffect(() => () => clearTimeout(timer.current), []);

  const show = (byHover = false) => {
    pinned.current = !byHover;
    setActive(selected >= 0 && !options[selected]?.disabled ? selected : (enabled[0] ?? -1));
    setOpen(true);
  };

  const hide = () => {
    clear();
    setOpen(false);
  };

  const leave = () => {
    if (!pinned.current) later(() => setOpen(false), closeDelay);
  };

  const onClick = () => {
    clear();
    if (!open) show();
    else if (pinned.current) hide();
    else pinned.current = true;
  };

  const later = (fn: () => void, ms: number) => {
    clear();
    timer.current = setTimeout(fn, ms);
  };

  const place = useCallback((el: HTMLUListElement | null) => {
    const anchor = button.current;
    if (!el || !anchor) return;
    const box = anchor.getBoundingClientRect();
    el.style.top = `${box.bottom + 2}px`;
    el.style.left = `${box.left}px`;
    el.style.minWidth = `${box.width}px`;
  }, []);

  useEffect(() => {
    if (!open) return;
    const close = () => setOpen(false);
    window.addEventListener('scroll', close, true);
    window.addEventListener('resize', close);
    return () => {
      window.removeEventListener('scroll', close, true);
      window.removeEventListener('resize', close);
    };
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
    const now = Date.now();
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

  const onKeyDown = (e: KeyboardEvent<HTMLButtonElement>) => {
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
        e.preventDefault();
        if (!open) show();
        move((e.key === 'Home' ? enabled[0] : enabled[enabled.length - 1]) ?? -1);
        return;
      case 'Enter':
      case ' ':
        e.preventDefault();
        if (open) choose(active);
        else show();
        return;
      case 'Escape':
        if (open) {
          e.preventDefault();
          e.stopPropagation();
          hide();
        }
        return;
      case 'Tab':
        hide();
        return;
    }
    if (e.key.length === 1) {
      e.preventDefault();
      typeahead(e.key);
    }
  };

  const optionId = (i: number) => `${id}-o${i}`;
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
        aria-activedescendant={open && active >= 0 ? optionId(active) : undefined}
        disabled={disabled}
        onClick={onClick}
        onKeyDown={onKeyDown}
        onBlur={() => later(() => setOpen(false), 0)}
      >
        <span>{options[selected]?.label ?? ''}</span>
        <span aria-hidden="true" className={styles.caret} />
      </button>
      {open &&
        createPortal(
          <ul
            ref={place}
            id={id}
            role="listbox"
            aria-label={label}
            className={styles.list}
            onMouseEnter={clear}
            onMouseLeave={leave}
            onMouseDown={(e) => e.preventDefault()}
          >
            {options.map((o, i) => (
              <li
                key={o.value}
                id={optionId(i)}
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
            ))}
          </ul>,
          document.body,
        )}
    </span>
  );
}
