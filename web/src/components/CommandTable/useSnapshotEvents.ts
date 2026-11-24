import { useQueryClient } from '@tanstack/react-query';
import { useEffect } from 'react';
import type { SyncStatus } from '@/api/client';
import { keys } from '@/api/queries';

function isSyncStatus(v: unknown): v is SyncStatus {
  return typeof v === 'object' && v !== null && typeof (v as SyncStatus).running === 'boolean';
}

/** Follows the server's event stream: a new snapshot refetches, a sync status is stored. */
export function useSnapshotEvents(): void {
  const client = useQueryClient();
  useEffect(() => {
    if (typeof EventSource === 'undefined') return;
    const source = new EventSource('/events');
    source.addEventListener('snapshot', () => {
      void client.invalidateQueries({ queryKey: keys.snapshot });
    });
    source.addEventListener('activity', () => {
      void client.invalidateQueries({ queryKey: keys.activity });
      void client.invalidateQueries({ queryKey: keys.retries });
    });
    source.addEventListener('sync-status', (e) => {
      try {
        const data: unknown = JSON.parse((e as MessageEvent<string>).data);
        if (isSyncStatus(data)) client.setQueryData(keys.syncStatus, data);
      } catch {
        // A malformed event is ignored; the next one replaces it.
      }
    });
    return () => source.close();
  }, [client]);
}
