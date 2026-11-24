import type { components, operations } from './schema';

export type Session = components['schemas']['Session'];
export type Problem = components['schemas']['Problem'];
type CreateSessionBody = components['schemas']['CreateSessionRequest'];

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

let csrfToken: string | undefined;

async function request(method: 'GET' | 'POST', path: string, body?: unknown): Promise<Response> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (method !== 'GET' && csrfToken) headers[CSRF_HEADER] = csrfToken;
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.ok) return res;
  const problem: unknown = await res.json().catch(() => undefined);
  throw new ApiError(res.status, isProblem(problem) ? problem.code : 'unknown');
}

type GetSessionResponse = operations['getSession']['responses'][200]['content']['application/json'];

export async function getSession(): Promise<Session> {
  const res = await request('GET', '/api/session');
  const session = (await res.json()) as GetSessionResponse;
  csrfToken = session.csrfToken;
  return session;
}

export async function createSession(launchToken: string): Promise<void> {
  const body: CreateSessionBody = { launchToken };
  await request('POST', '/api/session', body);
}
