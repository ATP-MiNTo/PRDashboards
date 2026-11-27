import { useCallback, useEffect, useState } from 'react';

/**
 * Whether an element has scrolled out of view, up behind the floating header pills. Hand `ref` to
 * the element; `out` is true once none of it is left below the top `offset` pixels. It watches with
 * an IntersectionObserver, so nothing runs on scroll events. Without the observer (old browsers,
 * tests) or without an element, `out` stays false.
 */
export function useStripOut(offset = 66) {
  const [el, setEl] = useState<Element | null>(null);
  const [out, setOut] = useState(false);
  const ref = useCallback((node: Element | null) => setEl(node), []);

  useEffect(() => {
    if (!el || typeof IntersectionObserver === 'undefined') return;
    const watch = new IntersectionObserver(
      (entries) => {
        const last = entries[entries.length - 1];
        // Out of view above the pills, not merely below the fold.
        if (last) setOut(!last.isIntersecting && last.boundingClientRect.bottom <= offset);
      },
      { rootMargin: `-${offset}px 0px 0px 0px` },
    );
    watch.observe(el);
    return () => watch.disconnect();
  }, [el, offset]);

  // The observer reports as soon as it watches, so a stale answer lasts no longer than a frame.
  return { ref, out: out && el !== null };
}
