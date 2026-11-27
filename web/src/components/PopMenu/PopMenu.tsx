'use client';

import {
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from 'react';
import { createPortal } from 'react-dom';
import { usePopover } from '../Dropdown/usePopover';
import styles from './PopMenu.module.css';

const OPEN_MS = 120;
const CLOSE_MS = 250;
const ITEMS = '[role="menuitem"]:not([aria-disabled="true"]), input, select';

export interface PopMenuProps {
  /** The accessible name of the trigger button. */
  label: string;
  /** What the trigger shows. */
  trigger: ReactNode;
  triggerClassName: string | undefined;
  /** The accessible name of the menu. */
  menuLabel: string;
  width: 'w44' | 'w48' | 'w72';
  side?: 'below' | 'above' | 'auto';
  /** Also open while the pointer rests on the trigger, as daisyUI's dropdown-hover does. */
  hover?: boolean;
  disabled?: boolean;
  /** Called each time the menu opens, for work that is only needed then (loading a list). */
  onOpen?: () => void;
  /** Extra text for assistive technology that describes the trigger. */
  describedBy?: string;
  children: (close: () => void) => ReactNode;
}

/**
 * A button that opens a menu in a portal: click, Enter, Space or the arrow keys open it (hover too,
 * when asked), the arrows move between items, Escape closes it and gives the focus back to the
 * button, a click outside or Tab closes it.
 */
export function PopMenu({
  label,
  trigger,
  triggerClassName,
  menuLabel,
  width,
  side = 'below',
  hover = false,
  disabled = false,
  onOpen,
  describedBy,
  children,
}: PopMenuProps) {
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pinned = useRef(false);
  const [open, setOpen] = useState(false);
  const { pop, place } = usePopover(open, button, setOpen, {
    side,
    gap: 4,
    matchWidth: false,
  });

  const clear = () => clearTimeout(timer.current);
  useEffect(() => () => clearTimeout(timer.current), []);
  const later = (fn: () => void, ms: number) => {
    clearTimeout(timer.current);
    timer.current = setTimeout(fn, ms);
  };

  const show = useCallback(
    (byHover: boolean) => {
      pinned.current = !byHover;
      setOpen(true);
      onOpen?.();
    },
    [onOpen],
  );
  const close = useCallback((back = true) => {
    clearTimeout(timer.current);
    setOpen(false);
    if (back) button.current?.focus();
  }, []);

  // A click anywhere else closes a menu that was opened on purpose.
  useEffect(() => {
    if (!open) return;
    const away = (e: Event) => {
      const at = e.target;
      if (!(at instanceof Node)) return;
      if (button.current?.contains(at) || pop.current?.contains(at)) return;
      close(false);
    };
    document.addEventListener('mousedown', away);
    return () => document.removeEventListener('mousedown', away);
  }, [open, close, pop]);

  // Opened by the keyboard or a click, the focus goes into the menu.
  useEffect(() => {
    if (!open || !pinned.current) return;
    const first = pop.current?.querySelector<HTMLElement>(ITEMS);
    (first ?? pop.current)?.focus();
  }, [open, pop]);

  const step = (dir: 1 | -1) => {
    const items = [...(pop.current?.querySelectorAll<HTMLElement>(ITEMS) ?? [])];
    if (items.length === 0) return;
    const at = items.indexOf(document.activeElement as HTMLElement);
    const next =
      at < 0 ? (dir === 1 ? 0 : items.length - 1) : (at + dir + items.length) % items.length;
    items[next]?.focus();
  };

  const onMenuKey = (e: KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown':
      case 'ArrowUp':
        e.preventDefault();
        step(e.key === 'ArrowDown' ? 1 : -1);
        return;
      case 'Escape':
        e.preventDefault();
        e.stopPropagation();
        close();
        return;
      case 'Tab':
        close(false);
        button.current?.focus();
    }
  };

  const onTriggerKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape' && open) {
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if ((e.key === 'ArrowDown' || e.key === 'ArrowUp') && !open) {
      e.preventDefault();
      show(false);
    }
  };

  const leave = () => {
    if (!pinned.current) later(() => setOpen(false), CLOSE_MS);
  };

  return (
    <span
      onMouseEnter={() => hover && !disabled && !open && later(() => show(true), OPEN_MS)}
      onMouseLeave={leave}
    >
      <button
        ref={button}
        type="button"
        className={triggerClassName}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-describedby={describedBy}
        disabled={disabled}
        onClick={() => {
          clear();
          if (!open) show(false);
          else if (pinned.current) close();
          else pinned.current = true;
        }}
        onKeyDown={onTriggerKey}
      >
        {trigger}
      </button>
      {open &&
        createPortal(
          <div
            ref={place}
            id={id}
            className={`${styles.menu} ${styles[width]}`}
            tabIndex={-1}
            onMouseEnter={clear}
            onMouseLeave={leave}
            onKeyDown={onMenuKey}
          >
            <ul role="menu" aria-label={menuLabel} className={styles.list}>
              <Items render={children} close={close} />
            </ul>
          </div>,
          document.body,
        )}
    </span>
  );
}

/** Renders the items of a menu with its close function. */
function Items({ render, close }: { render: (close: () => void) => ReactNode; close: () => void }) {
  return <>{render(close)}</>;
}

/** A heading row of a menu. */
export function MenuTitle({ children }: { children: ReactNode }) {
  return (
    <li role="presentation" className={styles.title}>
      {children}
    </li>
  );
}

/** A disabled text row of a menu, such as "No more tickets assigned to you". */
export function MenuNote({ children }: { children: ReactNode }) {
  return (
    <li role="presentation" className={styles.note}>
      {children}
    </li>
  );
}

/** One choice of a menu. `active` marks the current one with the neutral fill. */
export function MenuItem({
  onSelect,
  active = false,
  disabled = false,
  children,
}: {
  onSelect: () => void;
  active?: boolean;
  disabled?: boolean;
  children: ReactNode;
}) {
  return (
    <li role="none">
      <button
        type="button"
        role="menuitem"
        className={active ? `${styles.item} ${styles.active}` : styles.item}
        aria-disabled={disabled || undefined}
        onClick={() => !disabled && onSelect()}
      >
        {children}
      </button>
    </li>
  );
}

/** The class of a growing, ellipsised part of an item; and of an icon in front of it. */
export const menuParts = { grow: styles.grow, icon: styles.icon };
