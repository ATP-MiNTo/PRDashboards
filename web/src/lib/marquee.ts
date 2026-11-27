import { reducedMotion } from './storage';

const running = new WeakMap<HTMLElement, number>();

/** How long a slide takes: out at 45px per second (at least 0.8s), back at 12ms per pixel (at least 0.5s). */
export function slideDuration(distance: number, toEnd: boolean): number {
  return toEnd ? Math.max(800, (distance * 1000) / 45) : Math.max(500, distance * 12);
}

function cancel(el: HTMLElement) {
  const id = running.get(el);
  if (id !== undefined) cancelAnimationFrame(id);
  running.delete(el);
}

/** Moves the scroll position of `el` to `to` in a straight line. */
export function slideTo(el: HTMLElement, to: number): void {
  cancel(el);
  const from = el.scrollLeft;
  const dist = Math.abs(to - from);
  if (dist === 0) return;
  const finish = () => {
    running.delete(el);
    if (to === 0) el.removeAttribute('data-sliding');
  };
  if (reducedMotion()) {
    el.scrollLeft = to;
    finish();
    return;
  }
  const dur = slideDuration(dist, to !== 0);
  const t0 = performance.now();
  const step = (now: number) => {
    const k = Math.min(1, (now - t0) / dur);
    el.scrollLeft = from + (to - from) * k;
    if (k < 1) running.set(el, requestAnimationFrame(step));
    else finish();
  };
  running.set(el, requestAnimationFrame(step));
}

/** Truncated text slides to its end while the pointer is over it. Text that fits does nothing. */
export function slideOut(el: HTMLElement): void {
  const distance = el.scrollWidth - el.clientWidth;
  if (distance <= 0) return;
  el.setAttribute('data-sliding', '');
  slideTo(el, distance);
}

/** And slides back to the start when the pointer leaves. */
export function slideBack(el: HTMLElement): void {
  if (el.scrollLeft > 0 || running.has(el)) slideTo(el, 0);
}

export function stopSlide(el: HTMLElement): void {
  cancel(el);
}
