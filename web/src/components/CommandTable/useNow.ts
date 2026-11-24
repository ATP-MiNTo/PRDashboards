import { useSyncExternalStore } from 'react';

class Ticker {
  private now = Date.now();
  private readonly listeners = new Set<() => void>();
  private timer: ReturnType<typeof setInterval> | undefined;

  constructor(private readonly ms: number) {}

  readonly subscribe = (notify: () => void): (() => void) => {
    this.listeners.add(notify);
    if (this.timer === undefined) {
      this.now = Date.now();
      this.timer = setInterval(() => {
        this.now = Date.now();
        this.listeners.forEach((l) => l());
      }, this.ms);
    }
    return () => {
      this.listeners.delete(notify);
      if (this.listeners.size === 0 && this.timer !== undefined) {
        clearInterval(this.timer);
        this.timer = undefined;
      }
    };
  };

  readonly get = (): number => this.now;
}

const tickers = new Map<number, Ticker>();

function tickerFor(ms: number): Ticker {
  let t = tickers.get(ms);
  if (!t) {
    t = new Ticker(ms);
    tickers.set(ms, t);
  }
  return t;
}

const server = () => 0;

/** The current time, updated every `ms` milliseconds while a component uses it. */
export function useNow(ms: number): number {
  const t = tickerFor(ms);
  return useSyncExternalStore(t.subscribe, t.get, server);
}
