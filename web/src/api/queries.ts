import { useCallback } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  checkAccount,
  disconnectAccount,
  getAccounts,
  getAssignedTickets,
  getSnapshot,
  getSession,
  getSettings,
  getSetup,
  putGithubPat,
  putPrTickets,
  syncNow,
  putSettings,
  putSetupClient,
  revokeAllSessions,
  type ClientCredentials,
  type Provider,
  type Snapshot,
  type SnapshotPr,
  type SyncStatus,
  type TicketLinks,
  type Settings,
} from './client';

export const keys = {
  session: ['session'],
  accounts: ['accounts'],
  settings: ['settings'],
  setup: ['setup'],
  snapshot: ['snapshot'],
  assigned: ['assigned'],
  syncStatus: ['syncStatus'],
} as const;

export const useSyncStatus = () =>
  useQuery<SyncStatus | null>({
    queryKey: keys.syncStatus,
    queryFn: () => null,
    staleTime: Infinity,
  });
export const useSession = () => useQuery({ queryKey: keys.session, queryFn: getSession });
export const useAccounts = () => useQuery({ queryKey: keys.accounts, queryFn: getAccounts });
export const useSettings = () => useQuery({ queryKey: keys.settings, queryFn: getSettings });
export const useSetup = () => useQuery({ queryKey: keys.setup, queryFn: getSetup });

/** A mutation that refetches everything the page shows once it succeeds. */
function useAction<TVars, TResult>(fn: (vars: TVars) => Promise<TResult>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: TVars) => fn(vars),
    onSuccess: () => client.invalidateQueries({ predicate: (q) => q.queryKey[0] !== 'session' }),
  });
}

export const useSaveClient = () =>
  useAction(({ provider, ...credentials }: ClientCredentials & { provider: Provider }) =>
    putSetupClient(provider, credentials),
  );
export const useSavePat = () => useAction(putGithubPat);
export const useDisconnect = () => useAction(disconnectAccount);
export const useCheck = () => useAction(checkAccount);
export const useSaveSettings = () => useAction((settings: Settings) => putSettings(settings));
export const useRevokeAll = () => useAction(revokeAllSessions);

export const useSnapshot = <T = Snapshot>(select?: (s: Snapshot) => T) =>
  useQuery({ queryKey: keys.snapshot, queryFn: getSnapshot, select });

/** One pull request of the snapshot. The component re-renders only when this row changes. */
export const useSnapshotPr = (id: string) =>
  useSnapshot<SnapshotPr | undefined>(
    useCallback((s: Snapshot) => s.prs.find((p) => p.id === id), [id]),
  ).data;

export const useAssignedTickets = (enabled: boolean) =>
  useQuery({ queryKey: keys.assigned, queryFn: getAssignedTickets, enabled, staleTime: 60_000 });

export function useSyncNow() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: syncNow,
    onSuccess: (status) => client.setQueryData(keys.syncStatus, status),
  });
}

export function useSaveTickets() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, links }: { id: string; links: TicketLinks }) => putPrTickets(id, links),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.snapshot }),
  });
}
