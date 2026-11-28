import { useCallback } from 'react';
import {
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import {
  addGithubToken,
  approvePr,
  checkAccount,
  checkGithubToken,
  deleteGithubToken,
  renameGithubToken,
  markPrReady,
  requestChangesPr,
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
  type ApproveRequest,
  type ClientCredentials,
  type ReadyRequest,
  type RequestChangesRequest,
  type MergeRequest,
  type MergeResult,
  type RulesUpdate,
  type SyncRule,
  type RepoSettings,
  type RepoUpdate,
  type ReposUpdate,
  type Provider,
  type Snapshot,
  type SnapshotPr,
  type SyncStatus,
  type TicketLinks,
  type Settings,
} from './client';
import { repoTokensOf } from '@/lib/accounts';
import { prsById } from '@/lib/snapshot';

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

type QueryKey = readonly string[];

/**
 * A mutation that refetches the queries it changes once it succeeds, and no others. The sync status
 * has no fetcher (the event stream feeds it), so it is never listed here.
 */
function useAction<TVars, TResult>(
  fn: (vars: TVars) => Promise<TResult>,
  affects: readonly QueryKey[],
) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (vars: TVars) => fn(vars),
    onSuccess: () => Promise.all(affects.map((queryKey) => client.invalidateQueries({ queryKey }))),
  });
}

/** What a change of the connected accounts or tokens shows in: the accounts, the settings (a disconnected Jira site is dropped), the sources and repositories of the snapshot, and the Jira lists. */
const ACCOUNT_CHANGE = [
  keys.accounts,
  keys.settings,
  keys.snapshot,
  keys.repos,
  keys.assigned,
] as const;
/** A check of a token or an account refreshes its status and the sources of the snapshot. */
const ACCOUNT_CHECK = [keys.accounts, keys.snapshot] as const;

export const useSaveClient = () =>
  useAction(
    ({ provider, ...credentials }: ClientCredentials & { provider: Provider }) =>
      putSetupClient(provider, credentials),
    [keys.setup, keys.accounts],
  );
export const useSavePat = () => useAction(putGithubPat, ACCOUNT_CHANGE);
export const useAddToken = () => useAction(addGithubToken, ACCOUNT_CHANGE);
export const useRenameToken = () => useAction(renameGithubToken, [keys.accounts, keys.snapshot]);
export const useRemoveToken = () => useAction(deleteGithubToken, ACCOUNT_CHANGE);
export const useCheckToken = () => useAction(checkGithubToken, ACCOUNT_CHECK);
export const useDisconnect = () => useAction(disconnectAccount, ACCOUNT_CHANGE);
export const useCheck = () => useAction(checkAccount, ACCOUNT_CHECK);
/** The settings, and what they decide: the repositories' default method, the Jira site (the accounts say which one is selected) and the excluded statuses. */
export const useSaveSettings = () =>
  useAction(
    (settings: Settings) => putSettings(settings),
    [keys.settings, keys.accounts, keys.repos, keys.snapshot, keys.assigned, keys.activity],
  );
/** Every session is gone, so there is nothing to refetch. */
export const useRevokeAll = () => useAction(revokeAllSessions, []);

export const useSnapshot = <T = Snapshot>(select?: (s: Snapshot) => T) =>
  useQuery({ queryKey: keys.snapshot, queryFn: getSnapshot, select });

/** One pull request of the snapshot. The component re-renders only when this row changes. */
export const useSnapshotPr = (id: string) =>
  useSnapshot<SnapshotPr | undefined>(useCallback((s: Snapshot) => prsById(s).get(id), [id])).data;

/** Which token reads each repository, from the snapshot the page already has (no call of its own). */
export const useRepoTokens = () => useSnapshot(repoTokensOf);

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

/**
 * The update that changes only the approvals a repository needs (0 to 10): everything else is sent
 * as the repository has it now, and the repository stays marked new or seen as it was.
 */
export const approvalsUpdate = (repo: RepoSettings, approvalsRequired: number): RepoUpdate => ({
  defaultMethod: repo.defaultMethod,
  inheritMethod: repo.inheritMethod,
  deleteBranch: repo.deleteBranch,
  branchRules: repo.branchRules,
  clearNew: false,
  approvalsRequired,
});

/** Saves the approvals a repository needs. The snapshot follows: its `approvals.need` changes. */
export const useSaveApprovals = () =>
  useSaveReposWith(
    ({ repo, approvalsRequired }: { repo: RepoSettings; approvalsRequired: number }) =>
      putRepo(repo.id, approvalsUpdate(repo, approvalsRequired)),
  );

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

/** A merge made in this tab: the pull request as it was and the server's steps, for the History view. */
export interface RecentMerge {
  prId: string;
  /** The pull request when it was merged: the snapshot drops it soon after. */
  pr: SnapshotPr;
  result: MergeResult;
  /** When the server answered, in milliseconds. */
  at: number;
}

export const useRecentMerges = <T = RecentMerge[]>(select?: (m: RecentMerge[]) => T) =>
  useQuery({
    queryKey: keys.merges,
    queryFn: ({ client }) => client.getQueryData<RecentMerge[]>(keys.merges) ?? [],
    staleTime: Infinity,
    gcTime: Infinity,
    select,
  });

/** The History view keeps this many merges of the tab. */
const MAX_RECENT = 50;

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
      pr: SnapshotPr;
    }) => mergePr(id, key, body),
    onSuccess: (result, { id, pr }) =>
      client.setQueryData<RecentMerge[]>(keys.merges, (old = []) =>
        [{ prId: id, pr, result, at: Date.now() }, ...old.filter((m) => m.prId !== id)].slice(
          0,
          MAX_RECENT,
        ),
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

/**
 * One review action on one pull request. After it (success or refusal) the snapshot is fetched
 * again, so the card shows the new review state, or the newer head that made the server refuse.
 * The server starts one refresh of its own after a success; the browser makes no outside call.
 */
function useReviewAction<TBody>(fn: (id: string, body: TBody) => Promise<unknown>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: TBody }) => fn(id, body),
    onSettled: () => client.invalidateQueries({ queryKey: keys.snapshot }),
  });
}

export const useApprove = () => useReviewAction<ApproveRequest>(approvePr);
export const useRequestChanges = () => useReviewAction<RequestChangesRequest>(requestChangesPr);
export const useMarkReady = () => useReviewAction<ReadyRequest>(markPrReady);

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

/** Previews several rules, each against the cached data. The server makes no outside call for it. */
export const usePreviewRules = () =>
  useMutation({
    mutationFn: (rules: SyncRule[]) =>
      Promise.all(rules.map(async (rule) => ({ rule, preview: await previewRule(rule) }))),
  });

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
