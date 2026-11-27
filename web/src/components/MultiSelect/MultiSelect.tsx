'use client';

import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import { usePopover } from '../Dropdown/usePopover';
import styles from './MultiSelect.module.css';

export interface MultiSelectProps {
  /** The accessible name of the control. */
  label: string;
  values: string[];
  options: string[];
  onChange: (values: string[]) => void;
  disabled?: boolean;
  /** The most values that can be picked. */
  max?: number;
  /** The hint inside the empty search box. */
  placeholder?: string;
  /** Called whenever the popover opens, for callers that load the options on demand. */
  onOpen?: () => void;
}

const summary = (values: string[]) =>
  values.length === 0
    ? 'None'
    : values.length === 1
      ? values[0]
      : `${values[0]} +${values.length - 1}`;

/** A searchable multi-select: a trigger with a summary, a popover to pick from, and the picks as removable chips. */
export function MultiSelect({
  label,
  values,
  options,
  onChange,
  disabled = false,
  max,
  placeholder = 'Search',
  onOpen,
}: MultiSelectProps) {
  const id = useId();
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const { pop, place } = usePopover(open, trigger, setOpen);

  const needle = query.trim().toLowerCase();
  const shown = needle ? options.filter((o) => o.toLowerCase().includes(needle)) : options;
  const full = max !== undefined && values.length >= max;
  const optionId = (i: number) => `${id}-o${i}`;

  const close = useCallback(() => {
    setOpen(false);
    setQuery('');
  }, []);
  const show = () => {
    setQuery('');
    setActive(0);
    setOpen(true);
    onOpen?.();
  };

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      const at = e.target as Node;
      if (!pop.current?.contains(at) && !trigger.current?.contains(at)) close();
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open, pop, close]);

  useEffect(() => {
    if (open) document.getElementById(`${id}-o${active}`)?.scrollIntoView?.({ block: 'nearest' });
  }, [open, active, id]);

  const toggle = (name: string) => {
    if (values.includes(name)) onChange(values.filter((v) => v !== name));
    else if (!full) onChange([...values, name]);
  };

  const onSearchKey = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        e.preventDefault();
        setActive(
          Math.min(Math.max(active + (e.key === 'ArrowDown' ? 1 : -1), 0), shown.length - 1),
        );
        return;
      case ' ':
        // A space types into the search, except as the first character where it picks.
        if (query !== '') return;
      // falls through
      case 'Enter': {
        e.preventDefault();
        const name = shown[active];
        if (name !== undefined) toggle(name);
        return;
      }
      case 'Escape':
        e.preventDefault();
        e.stopPropagation();
        close();
        trigger.current?.focus();
        return;
      case 'Tab':
        close();
        // Focus the trigger first so the default Tab moves on from it, not from the portal.
        trigger.current?.focus();
    }
  };

  return (
    <div className={styles.root}>
      <button
        ref={trigger}
        type="button"
        role="combobox"
        className={styles.button}
        aria-label={label}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        disabled={disabled}
        onClick={() => (open ? close() : show())}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown' && !open) {
            e.preventDefault();
            show();
          }
        }}
      >
        <span>{summary(values)}</span>
        <span aria-hidden="true" className={styles.caret} />
      </button>
      {open &&
        createPortal(
          <div
            ref={place}
            className={styles.popover}
            // Keep the focus in the search box, except for a click into it.
            onMouseDown={(e) => (e.target as HTMLElement).tagName !== 'INPUT' && e.preventDefault()}
          >
            <input
              type="search"
              className={styles.search}
              aria-label={`Search ${label}`}
              aria-controls={id}
              aria-activedescendant={shown.length > 0 ? optionId(active) : undefined}
              placeholder={placeholder}
              autoComplete="off"
              autoFocus
              value={query}
              onChange={(e) => {
                setQuery(e.target.value);
                setActive(0);
              }}
              onKeyDown={onSearchKey}
            />
            <ul
              id={id}
              role="listbox"
              aria-label={label}
              aria-multiselectable="true"
              className={styles.list}
            >
              {shown.map((name, i) => {
                const on = values.includes(name);
                const blocked = !on && full;
                return (
                  <li
                    key={name}
                    id={optionId(i)}
                    role="option"
                    aria-selected={on}
                    aria-disabled={blocked || undefined}
                    className={[
                      styles.option,
                      i === active ? styles.active : '',
                      blocked ? styles.disabled : '',
                    ].join(' ')}
                    onMouseMove={() => active !== i && setActive(i)}
                    onClick={() => toggle(name)}
                  >
                    <span aria-hidden="true" className={on ? styles.check : styles.unchecked}>
                      ✓
                    </span>
                    {name}
                  </li>
                );
              })}
            </ul>
            {shown.length === 0 && <p className={styles.empty}>No matching status</p>}
          </div>,
          document.body,
        )}
      {values.length > 0 && (
        <ul className={styles.chips}>
          {values.map((v) => (
            <li key={v}>
              <button
                type="button"
                className={styles.chip}
                aria-label={`Remove ${v}`}
                disabled={disabled}
                onClick={() => onChange(values.filter((x) => x !== v))}
              >
                {v}
                <span aria-hidden="true"> ×</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
