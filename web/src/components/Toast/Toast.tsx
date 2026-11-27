'use client';

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import styles from './Toast.module.css';

export type ToastKind = 'info' | 'success' | 'error';

/** How long a toast stays on screen. */
export const TOAST_MS = 3200;

export interface ToastApi {
  /** Shows a message at the top right of the page. It leaves by itself after `TOAST_MS`. */
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

/** Provides `useToast` and renders the stack of toasts at the top right of the page. */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Item[]>([]);
  const next = useRef(0);
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());

  useEffect(() => {
    const all = timers.current;
    return () => all.forEach(clearTimeout);
  }, []);

  const show = useCallback((message: string, kind: ToastKind = 'info') => {
    const id = next.current++;
    setItems((list) => [...list, { id, message, kind }]);
    const timer = setTimeout(() => {
      timers.current.delete(timer);
      setItems((list) => list.filter((i) => i.id !== id));
    }, TOAST_MS);
    timers.current.add(timer);
  }, []);

  const api = useMemo(() => ({ show }), [show]);
  return (
    <ToastContext.Provider value={api}>
      {children}
      <div className={styles.stack} data-testid="toasts">
        {items.map((i) => (
          <div key={i.id} role="status" className={`${styles.toast} ${styles[i.kind]}`}>
            {i.message}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}
