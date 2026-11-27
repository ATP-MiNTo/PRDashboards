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

export type FreshPr = Schemas['FreshPr'];

export const CSRF_HEADER = 'X-PRD-CSRF';

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  /** The pull request as GitHub has it now, for a merge the server refused. */
  readonly pr: FreshPr | null;
  /** The problem's detail, shown only for codes that name what the person must change. */
  readonly detail: string | undefined;

  constructor(status: number, code: string, pr: FreshPr | null = null, detail?: string) {
    super(`${status} ${code}`);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.pr = pr;
    this.detail = detail;
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
  headers?: Record<string, string>;
}

async function request(method: Method, path: string, opts: RequestOptions = {}): Promise<Response> {
  if (method !== 'GET' && !csrfToken && !opts.withoutCsrf) await getSession();
  const headers: Record<string, string> = { ...opts.headers };
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
  const fresh = (problem as { pr?: FreshPr | null } | undefined)?.pr ?? null;
  throw new ApiError(
    res.status,
    isProblem(problem) ? problem.code : 'unknown',
    fresh,
    isProblem(problem) ? problem.detail : undefined,
  );
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
export type SourceStatus = Schemas['SourceStatus'];
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

export type MergeMethodName = Schemas['MergeMethodName'];
export type MethodResolution = Schemas['MethodResolution'];
export type BranchRule = Schemas['BranchRule'];
export type RepoSettings = Schemas['RepoSettings'];
export type Approvals = Schemas['Approvals'];
export type RepoUpdate = Schemas['RepoUpdate'];
export type DeleteBranchChoice = Schemas['DeleteBranchChoice'];
export type ReposUpdate = Schemas['ReposUpdate'];
export type JiraOptions = Schemas['JiraOptions'];
export type Transition = Schemas['Transition'];
export type MergeRequest = Schemas['MergeRequest'];
export type MergeResult = Schemas['MergeResult'];
export type MergeStep = Schemas['MergeStep'];
export type Retry = Schemas['Retry'];
export type RetryResult = Schemas['RetryResult'];
export type ActivityItem = Schemas['ActivityItem'];
export type ActivityPage = Schemas['ActivityPage'];

export const getRepos = () => json<Schemas['Repos']>('GET', '/api/repos');
export const putRepo = (id: string, update: RepoUpdate) =>
  json<RepoSettings>('PUT', `/api/repos/${encodeURIComponent(id)}`, update);
export const putRepos = (update: ReposUpdate) =>
  json<Schemas['Repos']>('PUT', '/api/repos', update);
export const getJiraOptions = () => json<JiraOptions>('GET', '/api/jira/options');

export async function mergePr(
  id: string,
  idempotencyKey: string,
  body: MergeRequest,
): Promise<MergeResult> {
  const res = await request('POST', `/api/prs/${encodeURIComponent(id)}/merge`, {
    body,
    headers: { 'Idempotency-Key': idempotencyKey },
  });
  return (await res.json()) as MergeResult;
}

export const getRetries = () => json<Schemas['Retries']>('GET', '/api/retries');
export const retryTransition = (id: number) => json<RetryResult>('POST', `/api/retries/${id}`);
export const dismissRetry = (id: number) => send('DELETE', `/api/retries/${id}`);
export const getActivity = (cursor?: number) =>
  json<ActivityPage>(
    'GET',
    cursor === undefined ? '/api/activity?limit=25' : `/api/activity?limit=25&cursor=${cursor}`,
  );

export type SyncRule = Schemas['SyncRule'];
export type RuleTrigger = Schemas['RuleTrigger'];
export type RuleMode = Schemas['RuleMode'];
export type RuleFilters = Schemas['RuleFilters'];
export type RulePreview = Schemas['RulePreview'];
export type RulesUpdate = Schemas['RulesUpdate'];
export type PrRef = Schemas['PrRef'];
export type TicketWaiting = Schemas['TicketWaiting'];
export type TicketMergePlan = Schemas['TicketMergePlan'];

export const getRules = () => json<Schemas['Rules']>('GET', '/api/rules');
export const putRules = (update: RulesUpdate) =>
  json<Schemas['Rules']>('PUT', '/api/rules', update);
export const previewRule = (rule: SyncRule) =>
  json<RulePreview>('POST', '/api/rules/preview', { rule });
/** One request for all rules: one preview per rule, in order. */
export const previewRules = (rules: SyncRule[]) =>
  json<Schemas['RulePreviews']>('POST', '/api/rules/previews', { rules });

export type ReviewResult = Schemas['ReviewResult'];
export type ApproveRequest = Schemas['ApproveRequest'];
export type RequestChangesRequest = Schemas['RequestChangesRequest'];
export type ReadyRequest = Schemas['ReadyRequest'];

/**
 * Approves a pull request. `expectedHeadSha` is the head the row showed; the server fetches the
 * pull request again and refuses with `head_moved` (409, with the fresh state in `ApiError.pr`) when
 * it differs. Other refusals: `not_open`, `own_pull_request`, `missing_permission` (403, the detail
 * names the permission), `review_rejected` (422), `rate_limited`, `upstream_error`.
 */
export const approvePr = (id: string, body: ApproveRequest) =>
  json<ReviewResult>('POST', `/api/prs/${encodeURIComponent(id)}/approve`, body);

/** Requests changes. `body` is required (not only white space), at most 65536 characters. */
export const requestChangesPr = (id: string, body: RequestChangesRequest) =>
  json<ReviewResult>('POST', `/api/prs/${encodeURIComponent(id)}/request-changes`, body);

/** Marks a draft ready for review. Also refuses with `not_draft`. */
export const markPrReady = (id: string, body: ReadyRequest) =>
  json<ReviewResult>('POST', `/api/prs/${encodeURIComponent(id)}/ready`, body);
