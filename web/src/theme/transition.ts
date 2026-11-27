/** Older browsers have no startViewTransition, whatever the DOM typings say. */
interface MaybeViewTransitions {
  startViewTransition?: (update: () => void) => { ready?: Promise<unknown> } | undefined;
}

/** What the page does to switch mode: the two attributes the palette and daisyUI follow. */
export interface ModeChange {
  mode: string;
  theme: string;
}

/**
 * Runs `change`, as a cross-fade of the whole page when the browser has View Transitions and the
 * user has not asked for reduced motion. The change always runs, so the mode switches either way.
 * The fade itself (0.9s) is styled in globals.css.
 */
export function crossfade(doc: Document, change: () => void, reduceMotion: boolean): void {
  const d = doc as unknown as MaybeViewTransitions;
  if (reduceMotion || typeof d.startViewTransition !== 'function') {
    change();
    return;
  }
  // A transition that is replaced by a newer one is skipped and its `ready` promise rejects. The
  // change still runs, so there is nothing to report.
  d.startViewTransition(change)?.ready?.catch(() => undefined);
}

/**
 * Sets data-mode and data-theme on the root element. The first call (no mode yet) and a call that
 * changes nothing apply at once. A real change between light and dark cross-fades.
 */
export function applyMode(doc: Document, next: ModeChange, reduceMotion: boolean): void {
  const root = doc.documentElement;
  const set = () => {
    root.dataset.mode = next.mode;
    root.dataset.theme = next.theme;
  };
  const current = root.dataset.mode;
  if (current === undefined || current === next.mode) {
    set();
    return;
  }
  crossfade(doc, set, reduceMotion);
}
