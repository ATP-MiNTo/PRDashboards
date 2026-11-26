import { useCallback, useEffect, useRef, type RefObject } from 'react';

/**
 * Places a fixed popover under its anchor and keeps it there: when the page scrolls (outside the
 * popover itself) or resizes, the popover is placed again. It closes only when the anchor has left
 * the viewport. A scroll caused by the browser or a test driver right after opening, such as
 * scrolling the anchor into view, therefore does not close it. Pass the returned `place` as the
 * popover's ref, `pop` reads the same element.
 */
export function usePopover(
  open: boolean,
  anchor: RefObject<HTMLElement | null>,
  setOpen: (open: boolean) => void,
) {
  const pop = useRef<HTMLDivElement | null>(null);

  const place = useCallback(
    (el: HTMLDivElement | null) => {
      pop.current = el;
      const host = anchor.current;
      if (!el || !host) return;
      const box = host.getBoundingClientRect();
      el.style.top = `${box.bottom + 2}px`;
      el.style.left = `${box.left}px`;
      el.style.minWidth = `${box.width}px`;
    },
    [anchor],
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
