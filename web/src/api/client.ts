import type { components, operations } from './schema';

type Schemas = components['schemas'];
export type Session = Schemas['Session'];
export type Problem = Schemas['Problem'];
export type Settings = Schemas['Settings'];
export type Setup = Schemas['Setup'];
export type Accounts = Schemas['Accounts'];
export type GithubAccount = Schemas['GithubAccount'];
export type JiraAccount = Schemas['JiraAccount'];
export type Provider = Schemas['Provider'];
export type ClientCredentials = Schemas['ClientCredentials'];
export type OAuthComplete = Schemas['OAuthComplete'];

export const CSRF_HEADER = 'X-PRD-CSRF';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string) {
    super(`${status} ${code}`);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

function isProblem(value: unknown): value is Problem {
  return (
    typeof value === 'object' &&
    value !== null &&
    typeof (value as Problem).code === 'string' &&
    typeof (value as Problem).status === 'number'
  );
}

type Method = 'GET' | 'POST' | 'PUT' | 'DELETE';

let csrfToken: string | undefined;

interface RequestOptions {
  body?: unknown;
  /** Skip fetching a CSRF token first. Only session creation has no session to ask. */
  withoutCsrf?: boolean;
}

async function request(method: Method, path: string, opts: RequestOptions = {}): Promise<Response> {
  if (method !== 'GET' && !csrfToken && !opts.withoutCsrf) await getSession();
  const headers: Record<string, string> = {};
  if (opts.body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && csrfToken) headers[CSRF_HEADER] = csrfToken;
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  });
  if (res.ok) return res;
  const problem: unknown = await res.json().catch(() => undefined);
  throw new ApiError(res.status, isProblem(problem) ? problem.code : 'unknown');
}

async function json<T>(method: Method, path: string, body?: unknown): Promise<T> {
  return (await request(method, path, { body })).json() as Promise<T>;
}

async function send(method: Method, path: string, body?: unknown): Promise<void> {
  await request(method, path, { body });
}

type GetSessionResponse = operations['getSession']['responses'][200]['content']['application/json'];

export async function getSession(): Promise<Session> {
  const res = await request('GET', '/api/session');
  const session = (await res.json()) as GetSessionResponse;
  csrfToken = session.csrfToken;
  return session;
}

export async function createSession(launchToken: string): Promise<void> {
  const body: Schemas['CreateSessionRequest'] = { launchToken };
  await request('POST', '/api/session', { body, withoutCsrf: true });
}

export const getSettings = () => json<Settings>('GET', '/api/settings');
export const putSettings = (settings: Settings) => json<Settings>('PUT', '/api/settings', settings);
export const getSetup = () => json<Setup>('GET', '/api/setup');
export const putSetupClient = (provider: Provider, credentials: ClientCredentials) =>
  send('PUT', `/api/setup/${provider}`, credentials);
export const putGithubPat = (token: string) =>
  json<GithubAccount>('PUT', '/api/github/pat', { token });
export const getAccounts = () => json<Accounts>('GET', '/api/accounts');
export const disconnectAccount = (provider: Provider) =>
  send('DELETE', `/api/accounts/${provider}`);
export const checkAccount = (provider: Provider) =>
  json<Accounts>('POST', `/api/accounts/${provider}/check`);
export const completeOAuth = (body: OAuthComplete) => send('POST', '/api/oauth/complete', body);
export const revokeAllSessions = () => send('POST', '/api/sessions/revoke-all');

export type Snapshot = Schemas['Snapshot'];
export type SnapshotPr = Schemas['SnapshotPr'];
export type SnapshotTicket = Schemas['SnapshotTicket'];
export type Check = Schemas['Check'];
export type SyncStatus = Schemas['SyncStatus'];
export type AssignedTickets = Schemas['AssignedTickets'];
export type TicketSummary = Schemas['TicketSummary'];
export type TicketLinks = Schemas['TicketLinks'];
export type Transitions = Schemas['Transitions'];

let snapshotEtag: string | undefined;
let snapshotCache: Snapshot | undefined;

/** Fetches the snapshot with If-None-Match, and returns the cached copy on 304. */
export async function getSnapshot(): Promise<Snapshot> {
  const headers: Record<string, string> = {};
  if (snapshotEtag && snapshotCache) headers['If-None-Match'] = snapshotEtag;
  const res = await fetch('/api/snapshot', { credentials: 'same-origin', headers });
  if (res.status === 304 && snapshotCache) return snapshotCache;
  if (!res.ok) {
    const problem: unknown = await res.json().catch(() => undefined);
    throw new ApiError(res.status, isProblem(problem) ? problem.code : 'unknown');
  }
  snapshotCache = (await res.json()) as Snapshot;
  snapshotEtag = res.headers.get('ETag') ?? undefined;
  return snapshotCache;
}

export const syncNow = () => json<SyncStatus>('POST', '/api/sync');
export const getAssignedTickets = () => json<AssignedTickets>('GET', '/api/tickets/assigned');
export const getTransitions = (key: string) =>
  json<Transitions>('GET', `/api/tickets/${encodeURIComponent(key)}/transitions`);
export const putPrTickets = (id: string, links: TicketLinks) =>
  send('PUT', `/api/prs/${encodeURIComponent(id)}/tickets`, links);
