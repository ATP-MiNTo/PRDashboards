'use client';

import {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type ReactNode,
} from 'react';
import styles from './Toast.module.css';

export type ToastKind = 'info' | 'success' | 'error';

/** How long an info or success toast stays on screen, not counting the time the pointer or the focus is on it. */
export const TOAST_MS = 3200;

export interface ToastApi {
  /**
   * Shows a message at the top right of the page. An info or success message leaves by itself after
   * `TOAST_MS`; an error stays until it is dismissed.
   */
  show: (message: string, kind?: ToastKind) => void;
}

interface Item {
  id: number;
  message: string;
  kind: ToastKind;
}

// Outside a provider a toast is dropped, so a component that toasts can render on its own.
const NONE: ToastApi = { show: () => {} };
const ToastContext = createContext<ToastApi>(NONE);

/** The toast function of the nearest `ToastProvider`. */
export const useToast = (): ToastApi => useContext(ToastContext);

interface ItemProps {
  item: Item;
  onDone: (id: number) => void;
}

/**
 * One toast. An info or success toast runs a timer that stops while the pointer is over it or the
 * focus is inside it, and goes on with the time that was left. An error has no timer and a close
 * button.
 */
const ToastItem = memo(function ToastItem({ item, onDone }: ItemProps) {
  const { id, kind, message } = item;
  const left = useRef(TOAST_MS);
  const since = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const held = useRef({ hover: false, focus: false });
  const timed = kind !== 'error';

  const start = useCallback(() => {
    since.current = Date.now();
    timer.current = setTimeout(() => onDone(id), left.current);
  }, [id, onDone]);
  const stop = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = undefined;
  }, []);

  useEffect(() => {
    if (!timed) return;
    start();
    return stop;
  }, [timed, start, stop]);

  const hold = (key: 'hover' | 'focus', on: boolean) => {
    if (!timed) return;
    const was = held.current.hover || held.current.focus;
    held.current[key] = on;
    const now = held.current.hover || held.current.focus;
    if (!was && now) {
      stop();
      left.current = Math.max(0, left.current - (Date.now() - since.current));
    } else if (was && !now) {
      start();
    }
  };

  const cls = `${styles.toast} ${styles[kind]}`;
  const handlers = {
    onMouseEnter: () => hold('hover', true),
    onMouseLeave: () => hold('hover', false),
    onFocus: () => hold('focus', true),
    onBlur: (e: FocusEvent<HTMLDivElement>) => {
      if (!e.currentTarget.contains(e.relatedTarget)) hold('focus', false);
    },
  };
  if (kind === 'error') {
    return (
      <div role="alert" className={cls}>
        <span>{message}</span>
        <button type="button" className={styles.close} onClick={() => onDone(id)}>
          <span aria-hidden="true">×</span>
          <span className={styles.sr}>Dismiss</span>
        </button>
      </div>
    );
  }
  return (
    <div className={cls} {...handlers}>
      {message}
    </div>
  );
});

/**
 * Provides `useToast` and renders the stack of toasts at the top right of the page. Info and
 * success toasts go into one live region that is always mounted (so a screen reader is already
 * listening when text arrives); an error is its own `role="alert"`.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Item[]>([]);
  const next = useRef(0);

  const show = useCallback((message: string, kind: ToastKind = 'info') => {
    const id = next.current++;
    setItems((list) => [...list, { id, message, kind }]);
  }, []);
  const done = useCallback((id: number) => {
    setItems((list) => list.filter((i) => i.id !== id));
  }, []);

  const api = useMemo(() => ({ show }), [show]);
  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className={styles.stack} data-testid="toasts">
        <div role="status" aria-live="polite" className={styles.live}>
          {items.map((i) => i.kind !== 'error' && <ToastItem key={i.id} item={i} onDone={done} />)}
        </div>
        {items.map((i) => i.kind === 'error' && <ToastItem key={i.id} item={i} onDone={done} />)}
      </div>
    </ToastContext.Provider>
  );
}
