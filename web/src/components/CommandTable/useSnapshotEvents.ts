import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import type { SyncStatus } from '@/api/client';
import { keys } from '@/api/queries';

/** The Web Lock one tab holds while it owns the event stream. */
export const LEADER_LOCK = 'prdash-events';
/** The channel the tab that owns the stream hands its events to the others on. */
export const EVENTS_CHANNEL = 'prdash-events';

type Message =
  | { type: 'snapshot' }
  | { type: 'activity' }
  | { type: 'sync-status'; data: SyncStatus }
  /** A tab that just joined asks for the last sync status, which the server replays only to the stream's owner. */
  | { type: 'hello' };

function isSyncStatus(v: unknown): v is SyncStatus {
  return typeof v === 'object' && v !== null && typeof (v as SyncStatus).running === 'boolean';
}

function parseMessage(v: unknown): Message | null {
  if (typeof v !== 'object' || v === null) return null;
  const m = v as { type?: unknown; data?: unknown };
  if (m.type === 'snapshot' || m.type === 'activity' || m.type === 'hello') return { type: m.type };
  if (m.type === 'sync-status' && isSyncStatus(m.data)) return { type: m.type, data: m.data };
  return null;
}

/** What an event does to the cache: a new snapshot refetches, activity refetches, a sync status is stored. */
function apply(client: QueryClient, m: Message): void {
  switch (m.type) {
    case 'snapshot':
      void client.invalidateQueries({ queryKey: keys.snapshot });
      break;
    case 'activity':
      void client.invalidateQueries({ queryKey: keys.activity });
      void client.invalidateQueries({ queryKey: keys.retries });
      break;
    case 'sync-status':
      client.setQueryData(keys.syncStatus, m.data);
      break;
    case 'hello':
      break;
  }
}

/** Opens `/events` and passes each event to `onEvent`. Returns the function that closes it. */
function openStream(onEvent: (m: Message) => void): () => void {
  const source = new EventSource('/events');
  source.addEventListener('snapshot', () => onEvent({ type: 'snapshot' }));
  source.addEventListener('activity', () => onEvent({ type: 'activity' }));
  source.addEventListener('sync-status', (e) => {
    try {
      const data: unknown = JSON.parse((e as MessageEvent<string>).data);
      if (isSyncStatus(data)) onEvent({ type: 'sync-status', data });
    } catch {
      // A malformed event is ignored; the next one replaces it.
    }
  });
  return () => source.close();
}

/**
 * One tab owns the event stream. The tab that gets the Web Lock opens `/events`, applies each
 * event and hands it to the other tabs over a BroadcastChannel; they apply the same cache update.
 * When the owner closes, its lock is released and the next tab in the queue takes over (the server
 * replays its last events to a new stream, so the new owner catches up on its own).
 */
function follow(client: QueryClient, locks: LockManager): () => void {
  const channel = new BroadcastChannel(EVENTS_CHANNEL);
  const abort = new AbortController();
  let stopStream: (() => void) | undefined;
  let release: (() => void) | undefined;
  let last: SyncStatus | null = null;

  channel.onmessage = (e: MessageEvent<unknown>) => {
    const m = parseMessage(e.data);
    if (!m) return;
    if (m.type === 'hello') {
      if (stopStream && last) channel.postMessage({ type: 'sync-status', data: last });
      return;
    }
    apply(client, m);
  };

  const lead = () =>
    new Promise<void>((resolve) => {
      release = resolve;
      stopStream = openStream((m) => {
        if (m.type === 'sync-status') last = m.data;
        apply(client, m);
        channel.postMessage(m);
      });
    });

  const queue = () =>
    locks.request(LEADER_LOCK, { signal: abort.signal }, lead).catch(() => {
      // Aborted when the component unmounts while it waits.
    });
  void locks
    .request(LEADER_LOCK, { ifAvailable: true }, (lock) => (lock ? lead() : queue()))
    .catch(() => {});

  channel.postMessage({ type: 'hello' });

  return () => {
    abort.abort();
    stopStream?.();
    stopStream = undefined;
    release?.();
    channel.close();
  };
}

/**
 * Follows the server's event stream: a new snapshot refetches, a sync status is stored. One
 * EventSource serves all the tabs of the browser (see `follow`); where Web Locks or
 * BroadcastChannel are missing, each tab opens its own.
 */
export function useSnapshotEvents(): void {
  const client = useQueryClient();
  useEffect(() => {
    if (typeof EventSource === 'undefined') return;
    const locks = typeof navigator === 'undefined' ? undefined : navigator.locks;
    if (!locks || typeof BroadcastChannel === 'undefined') {
      return openStream((m) => apply(client, m));
    }
    return follow(client, locks);
  }, [client]);
}
