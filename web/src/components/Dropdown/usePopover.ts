import { useCallback, useEffect, useRef, type RefObject } from 'react';

/**
 * Places a fixed popover under its anchor and closes it when the page scrolls (outside the popover
 * itself) or resizes. Pass the returned `place` as the popover's ref, `pop` reads the same element.
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
    const onScroll = (e: Event) => {
      if (!(e.target instanceof Node && pop.current?.contains(e.target))) setOpen(false);
    };
    const onResize = () => setOpen(false);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onResize);
    return () => {
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onResize);
    };
  }, [open, setOpen]);

  return { pop, place };
}
