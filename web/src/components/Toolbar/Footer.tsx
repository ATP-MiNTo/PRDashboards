import { useEffect, useRef, useState } from 'react';
import styles from './Footer.module.css';

export const REPO_URL = 'https://github.com/ATP-MiNTo/PRDashboards';
export const IDLE_MS = 400;
/** The floating button's size, and its gap to the corner of the page. */
const FAB = 44;
const EDGE = 16;
const LIFT = 20;

/**
 * Where the button goes: the footer slot once the footer is fully on screen, otherwise the bottom
 * right corner. Pure, so it is easy to test.
 */
export function fabPlacement(
  footer: { top: number; bottom: number },
  slot: { left: number; top: number; width: number },
  view: { width: number; height: number },
): { snapped: boolean; x: number; y: number; size: number } {
  const snapped = footer.bottom <= view.height + 1 && footer.top >= 0;
  return snapped
    ? { snapped, x: slot.left, y: slot.top, size: slot.width }
    : { snapped, x: view.width - FAB - EDGE, y: view.height - FAB - LIFT, size: FAB };
}

function GithubMark() {
  return (
    <svg
      className={styles.mark}
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

/** The page footer and the GitHub button that floats until the footer scrolls into view. */
export function Footer() {
  const footer = useRef<HTMLElement>(null);
  const slot = useRef<HTMLSpanElement>(null);
  const fab = useRef<HTMLAnchorElement>(null);
  const [snapped, setSnapped] = useState(false);
  const [ready, setReady] = useState(false);
  const [scrolling, setScrolling] = useState(false);

  useEffect(() => {
    let frame = 0;
    let idle: ReturnType<typeof setTimeout> | undefined;
    const place = () => {
      frame = 0;
      const f = footer.current;
      const s = slot.current;
      const b = fab.current;
      if (!f || !s || !b) return;
      const p = fabPlacement(f.getBoundingClientRect(), s.getBoundingClientRect(), {
        width: document.documentElement.clientWidth,
        height: window.innerHeight,
      });
      // Coordinates go through the CSSOM, so the exported HTML carries no style attribute.
      b.style.setProperty('--fab-x', `${p.x}px`);
      b.style.setProperty('--fab-y', `${p.y}px`);
      b.style.setProperty('--fab-size', `${p.size}px`);
      setSnapped(p.snapped);
    };
    const schedule = () => {
      if (frame === 0) frame = requestAnimationFrame(place);
    };
    const onScroll = () => {
      setScrolling(true);
      clearTimeout(idle);
      idle = setTimeout(() => setScrolling(false), IDLE_MS);
      schedule();
    };
    place();
    // Let the first position settle before transitions start, so the button does not fly in.
    const first = requestAnimationFrame(() => setReady(true));
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', schedule);
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(schedule);
    if (footer.current?.parentElement) observer?.observe(footer.current.parentElement);
    return () => {
      window.removeEventListener('scroll', onScroll);
      window.removeEventListener('resize', schedule);
      observer?.disconnect();
      cancelAnimationFrame(frame);
      cancelAnimationFrame(first);
      clearTimeout(idle);
    };
  }, []);

  const classes = [styles.fab];
  if (snapped) classes.push(styles.snapped);
  if (ready) classes.push(styles.ready);
  if (scrolling && !snapped) classes.push(styles.away);

  return (
    <>
      <footer ref={footer} className={styles.footer}>
        <span>
          Made with{' '}
          <span role="img" aria-label="love">
            💚
          </span>{' '}
          by ATP-MiNTo
        </span>
        <span className={styles.sep} aria-hidden="true" />
        <span ref={slot} className={styles.slot} />
      </footer>
      <a
        ref={fab}
        className={classes.join(' ')}
        href={REPO_URL}
        target="_blank"
        rel="noopener noreferrer"
        aria-label="GitHub repository"
      >
        <GithubMark />
      </a>
    </>
  );
}
