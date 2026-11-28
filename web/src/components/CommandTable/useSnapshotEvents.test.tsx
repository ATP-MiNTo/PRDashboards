import { QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { keys } from '@/api/queries';
import { makeQueryClient } from '@/app/providers';
import { EVENTS_CHANNEL, LEADER_LOCK, useSnapshotEvents } from './useSnapshotEvents';

class FakeEventSource {
  static all: FakeEventSource[] = [];
  closed = false;
  private handlers = new Map<string, ((e: MessageEvent<string>) => void)[]>();
  constructor(readonly url: string) {
    FakeEventSource.all.push(this);
  }
  addEventListener(name: string, fn: (e: MessageEvent<string>) => void) {
    this.handlers.set(name, [...(this.handlers.get(name) ?? []), fn]);
  }
  close() {
    this.closed = true;
  }
  emit(name: string, data: unknown = {}) {
    const e = { data: JSON.stringify(data) } as MessageEvent<string>;
    this.handlers.get(name)?.forEach((fn) => fn(e));
  }
}

/** Delivers to the other instances of the same name, never to the sender, as the real one does. */
class FakeChannel {
  static all: FakeChannel[] = [];
  onmessage: ((e: MessageEvent<unknown>) => void) | null = null;
  closed = false;
  constructor(readonly name: string) {
    FakeChannel.all.push(this);
  }
  postMessage(data: unknown) {
    for (const c of FakeChannel.all) {
      if (c !== this && !c.closed && c.name === this.name) c.onmessage?.({ data } as MessageEvent);
    }
  }
  close() {
    this.closed = true;
  }
}

/** One lock name, a queue of waiters, `ifAvailable` and `signal`. */
class FakeLocks {
  private held = false;
  private waiting: { run: () => void; signal?: AbortSignal }[] = [];
  requests: string[] = [];

  async request(
    name: string,
    options: { ifAvailable?: boolean; signal?: AbortSignal },
    cb: (lock: object | null) => Promise<unknown>,
  ): Promise<unknown> {
    this.requests.push(name);
    if (!this.held) return this.grant(cb);
    if (options.ifAvailable) return cb(null);
    return new Promise((resolve, reject) => {
      const waiter = { run: () => this.grant(cb).then(resolve, reject), signal: options.signal };
      options.signal?.addEventListener('abort', () => {
        this.waiting = this.waiting.filter((w) => w !== waiter);
        reject(new DOMException('aborted', 'AbortError'));
      });
      this.waiting.push(waiter);
    });
  }

  private async grant(cb: (lock: object | null) => Promise<unknown>) {
    this.held = true;
    try {
      return await cb({});
    } finally {
      this.held = false;
      this.waiting.shift()?.run();
    }
  }
}

function tab() {
  const client = makeQueryClient();
  const invalidate = vi.spyOn(client, 'invalidateQueries');
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  const view = renderHook(() => useSnapshotEvents(), { wrapper });
  return { client, invalidate, ...view };
}

const flush = () => act(async () => void (await Promise.resolve()));
const keysOf = (spy: ReturnType<typeof tab>['invalidate']) =>
  spy.mock.calls.map(([f]) => JSON.stringify(f?.queryKey));
const running = {
  running: true,
  githubAt: null,
  jiraAt: null,
  nextGithubAt: null,
  nextJiraAt: null,
};

beforeEach(() => {
  FakeEventSource.all = [];
  FakeChannel.all = [];
  vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => vi.unstubAllGlobals());

describe('without Web Locks or BroadcastChannel', () => {
  beforeEach(() => {
    vi.stubGlobal('navigator', { ...navigator, locks: undefined });
    vi.stubGlobal('BroadcastChannel', FakeChannel);
  });

  it('opens one stream per tab and applies its events', () => {
    const a = tab();
    const b = tab();
    expect(FakeEventSource.all).toHaveLength(2);
    expect(FakeChannel.all).toHaveLength(0);
    act(() => FakeEventSource.all[0]!.emit('snapshot'));
    expect(keysOf(a.invalidate)).toEqual([JSON.stringify(keys.snapshot)]);
    expect(b.invalidate).not.toHaveBeenCalled();
    act(() => FakeEventSource.all[1]!.emit('sync-status', running));
    expect(b.client.getQueryData(keys.syncStatus)).toEqual(running);
    a.unmount();
    expect(FakeEventSource.all[0]!.closed).toBe(true);
    b.unmount();
  });

  it('does the same without BroadcastChannel', () => {
    vi.stubGlobal('navigator', { ...navigator, locks: new FakeLocks() });
    vi.stubGlobal('BroadcastChannel', undefined);
    tab();
    tab();
    expect(FakeEventSource.all).toHaveLength(2);
  });
});

describe('with Web Locks and BroadcastChannel', () => {
  let locks: FakeLocks;
  beforeEach(() => {
    locks = new FakeLocks();
    vi.stubGlobal('navigator', { ...navigator, locks });
    vi.stubGlobal('BroadcastChannel', FakeChannel);
  });

  it('opens one stream for all the tabs, and the others apply what the leader hands over', async () => {
    const a = tab();
    await flush();
    const b = tab();
    const c = tab();
    await flush();
    expect(FakeEventSource.all).toHaveLength(1);
    expect(FakeEventSource.all[0]!.url).toBe('/events');
    expect(locks.requests.every((n) => n === LEADER_LOCK)).toBe(true);
    expect(FakeChannel.all.every((ch) => ch.name === EVENTS_CHANNEL)).toBe(true);

    const stream = FakeEventSource.all[0]!;
    act(() => stream.emit('snapshot', { version: 2 }));
    for (const t of [a, b, c])
      expect(keysOf(t.invalidate)).toEqual([JSON.stringify(keys.snapshot)]);

    act(() => stream.emit('activity'));
    for (const t of [a, b, c]) {
      expect(keysOf(t.invalidate)).toEqual([
        JSON.stringify(keys.snapshot),
        JSON.stringify(keys.activity),
        JSON.stringify(keys.retries),
      ]);
    }

    act(() => stream.emit('sync-status', running));
    for (const t of [a, b, c]) expect(t.client.getQueryData(keys.syncStatus)).toEqual(running);
  });

  it('ignores a malformed event, and a message that is not one of ours', async () => {
    const a = tab();
    const b = tab();
    await flush();
    act(() => FakeEventSource.all[0]!.emit('sync-status', { running: 'yes' }));
    act(() => FakeChannel.all[1]!.onmessage?.({ data: { type: 'nope' } } as MessageEvent));
    act(() => FakeChannel.all[1]!.onmessage?.({ data: null } as MessageEvent));
    expect(a.client.getQueryData(keys.syncStatus)).toBeUndefined();
    expect(b.client.getQueryData(keys.syncStatus)).toBeUndefined();
  });

  it('hands a tab that joins later the last sync status', async () => {
    tab();
    await flush();
    act(() => FakeEventSource.all[0]!.emit('sync-status', running));
    const late = tab();
    await flush();
    expect(late.client.getQueryData(keys.syncStatus)).toEqual(running);
    expect(FakeEventSource.all).toHaveLength(1);
  });

  it('passes the stream to the next tab when the leader closes', async () => {
    const a = tab();
    const b = tab();
    const c = tab();
    await flush();
    expect(FakeEventSource.all).toHaveLength(1);

    a.unmount();
    await flush();
    expect(FakeEventSource.all[0]!.closed).toBe(true);
    expect(FakeEventSource.all).toHaveLength(2);
    const next = FakeEventSource.all[1]!;
    expect(next.closed).toBe(false);

    act(() => next.emit('snapshot'));
    expect(keysOf(b.invalidate)).toContain(JSON.stringify(keys.snapshot));
    expect(keysOf(c.invalidate)).toContain(JSON.stringify(keys.snapshot));
    b.unmount();
    c.unmount();
  });

  it('a waiting tab that closes never opens a stream', async () => {
    tab();
    const b = tab();
    const c = tab();
    await flush();
    b.unmount();
    await flush();
    expect(FakeEventSource.all).toHaveLength(1);
    c.unmount();
  });

  it('closes its channel and stream on unmount', async () => {
    const a = tab();
    await flush();
    a.unmount();
    expect(FakeEventSource.all[0]!.closed).toBe(true);
    expect(FakeChannel.all[0]!.closed).toBe(true);
  });
});
