import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  checkAccount,
  disconnectAccount,
  getAccounts,
  getSession,
  getSettings,
  getSetup,
  putGithubPat,
  putSettings,
  putSetupClient,
  revokeAllSessions,
  type ClientCredentials,
  type Provider,
  type Settings,
} from './client';

export const keys = {
  session: ['session'],
  accounts: ['accounts'],
  settings: ['settings'],
  setup: ['setup'],
} as const;

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
