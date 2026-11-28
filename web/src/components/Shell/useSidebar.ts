'use client';

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { createIdleTimer } from './idleTimer';

export const SIDEBAR_KEY = 'prd-side';
export const WIDE_QUERY = '(min-width: 1024px)';
/** On a wide screen the sidebar folds after the pointer has been outside it this long. */
export const AUTO_COLLAPSE_MS = 30_000;

function readStored(): string | null {
  try {
    return localStorage.getItem(SIDEBAR_KEY);
  } catch {
    return null;
  }
}

function writeStored(open: boolean) {
  try {
    localStorage.setItem(SIDEBAR_KEY, open ? '1' : '0');
  } catch {
    // Storage can be blocked; the sidebar still works for this visit.
  }
}

const never = () => () => {};

/** Keys that are not an interaction by themselves. */
const MODIFIERS = new Set(['Shift', 'Control', 'Alt', 'Meta']);

function subscribeWide(onChange: () => void) {
  if (typeof window.matchMedia !== 'function') return () => {};
  const query = window.matchMedia(WIDE_QUERY);
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}

const isWide = () =>
  typeof window.matchMedia === 'function' && window.matchMedia(WIDE_QUERY).matches;

/**
 * Open state of the left sidebar. On a wide screen it is a column that is open by default and the
 * choice is saved in localStorage. On a narrow screen it is an overlay that starts closed and is not
 * saved. `hold` keeps the wide sidebar from auto-collapsing (a panel or modal is open).
 */
export function useSidebar({ hold = false }: { hold?: boolean } = {}) {
  const wide = useSyncExternalStore(subscribeWide, isWide, () => false);
  const stored = useSyncExternalStore(never, readStored, () => null);
  const [choice, setChoice] = useState<boolean | null>(null);
  const [folded, setFolded] = useState(false);
  const [overlayOpen, setOverlayOpen] = useState(false);
  const aside = useRef<HTMLElement | null>(null);
  const holdRef = useRef(hold);
  useEffect(() => {
    holdRef.current = hold;
  });

  const wideOpen = (choice ?? stored !== '0') && !folded;
  const open = wide ? wideOpen : overlayOpen;

  const setOpen = useCallback(
    (next: boolean) => {
      if (wide) {
        setFolded(false);
        setChoice(next);
        writeStored(next);
      } else {
        setOverlayOpen(next);
      }
    },
    [wide],
  );

  const toggle = useCallback(() => setOpen(!open), [open, setOpen]);
  const close = useCallback(() => setOpen(false), [setOpen]);

  const autoCollapse = wide && wideOpen;
  useEffect(() => {
    if (!autoCollapse) return;
    // Focus in the sidebar holds it open only for a keyboard user. A button the pointer clicked
    // keeps the focus after the pointer has left, and that must not keep the sidebar open for good.
    let keyboard = false;
    let inside = false;
    const focusHeld = () => keyboard && !!aside.current?.contains(document.activeElement);
    const timer = createIdleTimer({
      delayMs: AUTO_COLLAPSE_MS,
      onIdle: () => setFolded(true),
      // A dialog over the page (the repository settings, a note, the rules) keeps the sidebar open too.
      isHeld: () =>
        holdRef.current ||
        focusHeld() ||
        document.querySelector('[role="dialog"][aria-modal="true"]') !== null,
    });
    const onKeyDown = (e: KeyboardEvent) => {
      if (!MODIFIERS.has(e.key)) keyboard = true;
    };
    const onPointerDown = () => {
      keyboard = false;
    };
    const onMove = (e: MouseEvent) => {
      // A moving pointer is the person at the mouse: focus they left in the sidebar holds nothing.
      keyboard = false;
      if (aside.current?.contains(e.target as Node)) {
        inside = true;
        timer.cancel();
      } else if (inside || !timer.isArmed()) {
        inside = false;
        timer.arm();
      }
    };
    const onLeave = () => {
      inside = false;
      if (!timer.isArmed()) timer.arm();
    };
    const onFocusIn = (e: FocusEvent) => {
      if (keyboard && aside.current?.contains(e.target as Node)) timer.cancel();
    };
    const onFocusOut = () => {
      if (!inside && !timer.isArmed()) timer.arm();
    };
    document.addEventListener('keydown', onKeyDown, true);
    document.addEventListener('pointerdown', onPointerDown, true);
    document.addEventListener('mousemove', onMove);
    document.documentElement.addEventListener('mouseleave', onLeave);
    document.addEventListener('focusin', onFocusIn);
    document.addEventListener('focusout', onFocusOut);
    timer.arm();
    return () => {
      timer.cancel();
      document.removeEventListener('keydown', onKeyDown, true);
      document.removeEventListener('pointerdown', onPointerDown, true);
      document.removeEventListener('mousemove', onMove);
      document.documentElement.removeEventListener('mouseleave', onLeave);
      document.removeEventListener('focusin', onFocusIn);
      document.removeEventListener('focusout', onFocusOut);
    };
  }, [autoCollapse]);

  return { wide, open, toggle, close, aside };
}

/** True once the page has scrolled past `threshold` pixels. */
export function useScrolled(threshold = 24) {
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const update = () => setScrolled(window.scrollY > threshold);
    update();
    window.addEventListener('scroll', update, { passive: true });
    return () => window.removeEventListener('scroll', update);
  }, [threshold]);
  return scrolled;
}
