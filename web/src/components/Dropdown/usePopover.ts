import { useCallback, useEffect, useRef, type RefObject } from 'react';

export interface PopoverPlacement {
  /** Below the anchor, above it, or below unless there is no room (then above). Default below. */
  side?: 'below' | 'above' | 'auto';
  /** Which edge of the anchor the popover lines up with. Default start (the left edge). */
  align?: 'start' | 'end';
  /** Pixels between the anchor and the popover. Default 2. */
  gap?: number;
  /** Whether the popover is at least as wide as the anchor. Default true. */
  matchWidth?: boolean;
  /** Pixels added to the left edge, before the popover is kept inside the viewport. Default 0. */
  shift?: number;
}

const MARGIN = 8;

/**
 * Places a fixed popover next to its anchor and keeps it there: when the page scrolls (outside the
 * popover itself) or resizes, the popover is placed again. It closes only when the anchor has left
 * the viewport. A scroll caused by the browser or a test driver right after opening, such as
 * scrolling the anchor into view, therefore does not close it. Pass the returned `place` as the
 * popover's ref, `pop` reads the same element. The popover is kept inside the viewport sideways.
 */
export function usePopover(
  open: boolean,
  anchor: RefObject<HTMLElement | null>,
  setOpen: (open: boolean) => void,
  placement: PopoverPlacement = {},
) {
  const pop = useRef<HTMLDivElement | null>(null);
  const { side = 'below', align = 'start', gap = 2, matchWidth = true, shift = 0 } = placement;

  const place = useCallback(
    (el: HTMLDivElement | null) => {
      pop.current = el;
      const host = anchor.current;
      if (!el || !host) return;
      const box = host.getBoundingClientRect();
      if (matchWidth) el.style.minWidth = `${box.width}px`;
      const height = el.offsetHeight;
      const width = el.offsetWidth;
      const above =
        side === 'above' ||
        (side === 'auto' &&
          box.bottom + gap + height > window.innerHeight &&
          box.top - gap - height >= 0);
      el.style.top = `${above ? box.top - gap - height : box.bottom + gap}px`;
      const left = (align === 'end' ? box.right - width : box.left) + shift;
      const max = Math.max(MARGIN, window.innerWidth - width - MARGIN);
      el.style.left = `${Math.max(MARGIN, Math.min(left, max))}px`;
    },
    [anchor, side, align, gap, matchWidth, shift],
  );

  useEffect(() => {
    if (!open) return;
    const follow = () => {
      const host = anchor.current;
      if (!host) return;
      const box = host.getBoundingClientRect();
      const vh = window.innerHeight;
      const vw = window.innerWidth;
      if (box.bottom < 0 || box.top > vh || box.right < 0 || box.left > vw) {
        setOpen(false);
        return;
      }
      place(pop.current);
    };
    const onScroll = (e: Event) => {
      if (!(e.target instanceof Node && pop.current?.contains(e.target))) follow();
    };
    const onResize = follow;
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onResize);
    };
  }, [open, setOpen, anchor, place]);

  return { pop, place };
}
