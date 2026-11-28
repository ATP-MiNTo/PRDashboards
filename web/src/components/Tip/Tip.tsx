import type { FocusEvent, PointerEvent, ReactNode } from 'react';
import styles from './Tip.module.css';

const EDGE = 8;
const MAX = 320;

/**
 * Places the bubble before it shows: centred over the wrapper when there is room on both sides,
 * grown inwards from the wrapper's edge near the left or right edge of the viewport. `--tip-max`
 * keeps it narrower than the room it has.
 */
export function placeTip(el: HTMLElement, side: 'top' | 'left', viewport: number) {
  const r = el.getBoundingClientRect();
  if (viewport <= 0 || (r.width === 0 && r.left === 0)) return;
  let align = 'center';
  let room: number;
  if (side === 'left') {
    room = r.left - EDGE;
  } else {
    const mid = r.left + r.width / 2;
    const half = Math.min(mid, viewport - mid) - EDGE;
    if (half * 2 >= MAX) {
      room = MAX;
    } else if (mid < viewport - mid) {
      align = 'start';
      room = viewport - r.left - EDGE;
    } else {
      align = 'end';
      room = r.right - EDGE;
    }
  }
  el.dataset.align = align;
  el.style.setProperty('--tip-max', `${Math.max(0, Math.min(MAX, room))}px`);
}

/**
 * The design's tooltip around one control. `tip` is shown on hover and while a control inside has
 * keyboard focus. It is only a visual hint: the control must also carry the text for assistive
 * technology (an `aria-describedby` to the same text, or a visible label).
 */
export function Tip({
  tip,
  side = 'top',
  className,
  children,
}: {
  tip: string;
  side?: 'top' | 'left';
  className?: string;
  children: ReactNode;
}) {
  const cls = [styles.tip, side === 'left' ? styles.left : '', className ?? '']
    .filter(Boolean)
    .join(' ');
  const place = (e: PointerEvent<HTMLElement> | FocusEvent<HTMLElement>) =>
    placeTip(e.currentTarget, side, document.documentElement.clientWidth || window.innerWidth);
  return (
    <span className={cls} data-tip={tip} onPointerEnter={place} onFocus={place}>
      {children}
    </span>
  );
}
