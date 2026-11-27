export interface IdleTimerOptions {
  delayMs: number;
  /** Called when the delay passes and nothing holds the timer. */
  onIdle: () => void;
  /** True while something (focus, an open panel) should keep the target open; the timer then starts over. */
  isHeld: () => boolean;
}

export interface IdleTimer {
  /** Starts the delay, replacing a running one. */
  arm: () => void;
  cancel: () => void;
  isArmed: () => boolean;
}

/** A one-shot timer that fires `onIdle` after `delayMs` unless it is cancelled or held. */
export function createIdleTimer({ delayMs, onIdle, isHeld }: IdleTimerOptions): IdleTimer {
  let id: ReturnType<typeof setTimeout> | undefined;
  const cancel = () => {
    if (id !== undefined) clearTimeout(id);
    id = undefined;
  };
  const arm = () => {
    cancel();
    id = setTimeout(() => {
      id = undefined;
      if (isHeld()) arm();
      else onIdle();
    }, delayMs);
  };
  return { arm, cancel, isArmed: () => id !== undefined };
}
