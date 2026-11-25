import { useCallback } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import {
  checkAccount,
  dismissRetry,
  getActivity,
  getJiraOptions,
  getRepos,
  putRepos,
  getRules,
  previewRule,
  putRules,
  getRetries,
  getTransitions,
  mergePr,
  putRepo,
  retryTransition,
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
  type MergeRequest,
  type MergeResult,
  type RulesUpdate,
  type SyncRule,
  type RepoUpdate,
  type ReposUpdate,
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
  repos: ['repos'],
  retries: ['retries'],
  activity: ['activity'],
  transitions: ['transitions'],
  merges: ['merges'],
  rules: ['rules'],
  jiraOptions: ['jiraOptions'],
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
    onSuccess: () =>
      client.invalidateQueries({
        predicate: (q) => q.queryKey[0] !== 'session' && q.queryKey[0] !== keys.jiraOptions[0],
      }),
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

/** The status follows the event stream, which reports both the start and the end of a refresh. The response is not stored, because a fast refresh can finish before it arrives. */
export const useSyncNow = () => useMutation({ mutationFn: syncNow });

export function useSaveTickets() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, links }: { id: string; links: TicketLinks }) => putPrTickets(id, links),
    onSuccess: () => client.invalidateQueries({ queryKey: keys.snapshot }),
  });
}

export const useRepos = (enabled = true) =>
  useQuery({ queryKey: keys.repos, queryFn: getRepos, enabled });

function useSaveReposWith<TVars>(fn: (vars: TVars) => Promise<unknown>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: keys.repos }),
        client.invalidateQueries({ queryKey: keys.snapshot }),
      ]),
  });
}

export const useSaveRepo = () =>
  useSaveReposWith(({ id, update }: { id: string; update: RepoUpdate }) => putRepo(id, update));

/** Saves several repositories in one request. */
export const useSaveRepos = () => useSaveReposWith((update: ReposUpdate) => putRepos(update));

/** The transitions of the tickets that are being moved. Other tickets cost no outside call. */
export const useTicketTransitions = (ticketKeys: { key: string; enabled: boolean }[]) =>
  useQueries({
    queries: ticketKeys.map(({ key, enabled }) => ({
      queryKey: [...keys.transitions, key],
      queryFn: () => getTransitions(key),
      enabled,
      staleTime: 30_000,
    })),
  });

export const useRetries = () => useQuery({ queryKey: keys.retries, queryFn: getRetries });

export const useActivity = (enabled: boolean) =>
  useInfiniteQuery({
    queryKey: keys.activity,
    queryFn: ({ pageParam }) => getActivity(pageParam),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.nextCursor ?? undefined,
    enabled,
  });

/** The outcome of a merge made in this tab, kept for the steps list and the recent merges strip. */
export interface RecentMerge {
  prId: string;
  label: string;
  result: MergeResult;
}

export const useRecentMerges = <T = RecentMerge[]>(select?: (m: RecentMerge[]) => T) =>
  useQuery({
    queryKey: keys.merges,
    queryFn: ({ client }) => client.getQueryData<RecentMerge[]>(keys.merges) ?? [],
    staleTime: Infinity,
    gcTime: Infinity,
    select,
  });

const MAX_RECENT = 10;

/** Merges a pull request. The key identifies one panel opening, so a repeated click replays. */
export function useMerge() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      key,
      body,
    }: {
      id: string;
      key: string;
      body: MergeRequest;
      label: string;
    }) => mergePr(id, key, body),
    onSuccess: (result, { id, label }) =>
      client.setQueryData<RecentMerge[]>(keys.merges, (old = []) =>
        [{ prId: id, label, result }, ...old.filter((m) => m.prId !== id)].slice(0, MAX_RECENT),
      ),
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: keys.snapshot }),
        client.invalidateQueries({ queryKey: keys.retries }),
        client.invalidateQueries({ queryKey: keys.activity }),
        client.invalidateQueries({ queryKey: keys.transitions, refetchType: 'none' }),
      ]),
  });
}

export function useRetryTransition() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: retryTransition,
    onSuccess: ({ step }, id) =>
      client.setQueryData<RecentMerge[]>(keys.merges, (old = []) =>
        old.map((m) => ({
          ...m,
          result: {
            ...m.result,
            steps: m.result.steps.map((s) => (s.retryId === id ? step : s)),
          },
        })),
      ),
    onSettled: () =>
      Promise.all([
        client.invalidateQueries({ queryKey: keys.snapshot }),
        client.invalidateQueries({ queryKey: keys.retries }),
        client.invalidateQueries({ queryKey: keys.activity }),
        client.invalidateQueries({ queryKey: keys.transitions, refetchType: 'none' }),
      ]),
  });
}

export function useDismissRetry() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: dismissRetry,
    onSuccess: () => client.invalidateQueries({ queryKey: keys.retries }),
  });
}

export const useRules = (enabled = true) =>
  useQuery({ queryKey: keys.rules, queryFn: getRules, enabled });

/** Saves the whole ordered list. The server evaluates the rules again, so the snapshot and the log follow. */
export function useSaveRules() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (update: RulesUpdate) => putRules(update),
    onSuccess: (saved) => {
      client.setQueryData(keys.rules, saved);
      return Promise.all([
        client.invalidateQueries({ queryKey: keys.snapshot }),
        client.invalidateQueries({ queryKey: keys.activity }),
        client.invalidateQueries({ queryKey: keys.retries }),
      ]);
    },
  });
}

export const usePreviewRule = () =>
  useMutation({ mutationFn: (rule: SyncRule) => previewRule(rule) });

/**
 * The Jira statuses and project keys for the rule form. It is read on demand and kept for a few
 * minutes, never refetched on a timer or on focus.
 */
export const useJiraOptions = (enabled: boolean) =>
  useQuery({
    queryKey: keys.jiraOptions,
    queryFn: getJiraOptions,
    enabled,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  });
