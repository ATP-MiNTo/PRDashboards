import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as client from './client';
import { ApiError, CSRF_HEADER, createSession, getSession } from './client';

const session = { csrfToken: 'csrf-1', expiresAt: '2026-12-24T00:00:00Z', version: '0.1.0' };

function json(body: unknown, status = 200, type = 'application/json'): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': type } });
}

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
  fetchMock.mockReset();
  vi.unstubAllGlobals();
});

describe('api client', () => {
  it('gets the session with same-origin credentials', async () => {
    fetchMock.mockResolvedValueOnce(json(session));
    await expect(getSession()).resolves.toEqual(session);
    const [path, init] = fetchMock.mock.calls[0]!;
    expect(path).toBe('/api/session');
    expect(init?.method).toBe('GET');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.body).toBeUndefined();
  });

  it('posts the launch token as JSON and sends the CSRF header once known', async () => {
    fetchMock.mockResolvedValueOnce(json(session));
    await getSession();
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await createSession('tok');
    const [path, init] = fetchMock.mock.calls[1]!;
    expect(path).toBe('/api/session');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(init?.body).toBe(JSON.stringify({ launchToken: 'tok' }));
    expect(init?.headers).toMatchObject({
      'Content-Type': 'application/json',
      [CSRF_HEADER]: 'csrf-1',
    });
  });

  it('throws ApiError with the problem code', async () => {
    fetchMock.mockResolvedValueOnce(
      json(
        {
          type: '/problems/unauthenticated',
          title: 'Unauthorized',
          status: 401,
          code: 'unauthenticated',
        },
        401,
        'application/problem+json',
      ),
    );
    const error = await getSession().catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 401, code: 'unauthenticated' });
  });

  it('keeps the detail of the problem', async () => {
    fetchMock.mockResolvedValueOnce(
      json(
        {
          type: '/problems/x',
          title: 'Forbidden',
          status: 403,
          code: 'missing_permission',
          detail: 'contents: write',
        },
        403,
        'application/problem+json',
      ),
    );
    await expect(getSession()).rejects.toMatchObject({
      code: 'missing_permission',
      detail: 'contents: write',
    });
  });

  it('falls back to an unknown code for non-problem errors', async () => {
    fetchMock.mockResolvedValueOnce(new Response('<html>', { status: 502 }));
    await expect(getSession()).rejects.toMatchObject({ status: 502, code: 'unknown' });
    fetchMock.mockResolvedValueOnce(json({ message: 'x' }, 500));
    await expect(getSession()).rejects.toMatchObject({ status: 500, code: 'unknown' });
  });

  it('propagates network failures', async () => {
    fetchMock.mockRejectedValueOnce(new TypeError('network'));
    await expect(createSession('tok')).rejects.toThrow('network');
  });
});

describe('api client operations', () => {
  it('fetches a CSRF token before the first write', async () => {
    vi.resetModules();
    const fresh = await import('./client');
    fetchMock
      .mockResolvedValueOnce(json(session))
      .mockResolvedValueOnce(new Response(null, { status: 204 }));
    await fresh.revokeAllSessions();
    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      '/api/session',
      '/api/sessions/revoke-all',
    ]);
    expect(fetchMock.mock.calls[1]![1]?.headers).toMatchObject({ [CSRF_HEADER]: 'csrf-1' });
  });

  it('does not ask for a session to create one', async () => {
    vi.resetModules();
    const fresh = await import('./client');
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await fresh.createSession('tok');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['getSettings', [], 'GET', '/api/settings', undefined],
    ['getSetup', [], 'GET', '/api/setup', undefined],
    ['getJiraOptions', [], 'GET', '/api/jira/options', undefined],
    [
      'putRepos',
      [
        {
          repos: [
            { id: 'R1', update: { defaultMethod: 'merge', branchRules: [], clearNew: true } },
          ],
        },
      ],
      'PUT',
      '/api/repos',
      {
        repos: [{ id: 'R1', update: { defaultMethod: 'merge', branchRules: [], clearNew: true } }],
      },
    ],
    ['getAccounts', [], 'GET', '/api/accounts', undefined],
    ['putGithubPat', ['ghp_x'], 'PUT', '/api/github/pat', { token: 'ghp_x' }],
    ['disconnectAccount', ['atlassian'], 'DELETE', '/api/accounts/atlassian', undefined],
    ['checkAccount', ['github'], 'POST', '/api/accounts/github/check', undefined],
    ['revokeAllSessions', [], 'POST', '/api/sessions/revoke-all', undefined],
    [
      'putSetupClient',
      ['github', { clientId: 'a', clientSecret: 'b' }],
      'PUT',
      '/api/setup/github',
      { clientId: 'a', clientSecret: 'b' },
    ],
    [
      'completeOAuth',
      [{ provider: 'github', code: 'c', state: 's' }],
      'POST',
      '/api/oauth/complete',
      { provider: 'github', code: 'c', state: 's' },
    ],
  ] as const)('%s sends %s %s', async (name, args, method, path, body) => {
    fetchMock.mockResolvedValueOnce(json(session));
    await getSession();
    fetchMock.mockImplementationOnce(async () =>
      method === 'GET' || name === 'putGithubPat' || name === 'checkAccount' || name === 'putRepos'
        ? json({})
        : new Response(null, { status: 204 }),
    );
    const call = client[name] as (...a: unknown[]) => Promise<unknown>;
    await call(...args);
    const [url, init] = fetchMock.mock.calls.at(-1)!;
    expect(url).toBe(path);
    expect(init?.method).toBe(method);
    expect(init?.credentials).toBe('same-origin');
    expect(init?.body).toBe(body === undefined ? undefined : JSON.stringify(body));
  });

  it('puts the settings and returns the saved ones', async () => {
    const saved = {
      polling: { githubSec: 60, jiraSec: 300 },
      theme: 'auto',
      defaultMethodForNewRepos: 'squash',
      deleteBranchAfterMerge: false,
      multiPrTickets: 'all',
      jiraSiteId: null,
      excludedStatuses: ['Done'] as string[],
    } as const;
    fetchMock.mockResolvedValueOnce(json(session));
    await getSession();
    fetchMock.mockResolvedValueOnce(json(saved));
    await expect(client.putSettings(saved)).resolves.toEqual(saved);
  });
});

describe('review actions', () => {
  const sha = '0123456789abcdef0123456789abcdef01234567';

  async function signedIn() {
    fetchMock.mockResolvedValueOnce(json(session));
    await getSession();
  }

  it.each([
    ['approvePr', 'approve', { expectedHeadSha: sha, body: 'LGTM' }, 'approve'],
    ['approvePr', 'approve', { expectedHeadSha: sha }, 'approve'],
    [
      'requestChangesPr',
      'request-changes',
      { expectedHeadSha: sha, body: 'rename it' },
      'request_changes',
    ],
    ['markPrReady', 'ready', { expectedHeadSha: sha }, 'ready'],
  ] as const)(
    '%s posts to /api/prs/{id}/%s with the CSRF header',
    async (name, path, body, action) => {
      await signedIn();
      fetchMock.mockResolvedValueOnce(json({ prId: 'PR_1/x', action }));
      const call = client[name] as (id: string, b: unknown) => Promise<unknown>;
      await expect(call('PR_1/x', body)).resolves.toEqual({ prId: 'PR_1/x', action });
      const [url, init] = fetchMock.mock.calls.at(-1)!;
      expect(url).toBe(`/api/prs/PR_1%2Fx/${path}`);
      expect(init?.method).toBe('POST');
      expect(init?.credentials).toBe('same-origin');
      expect(init?.body).toBe(JSON.stringify(body));
      expect(init?.headers).toMatchObject({
        [CSRF_HEADER]: 'csrf-1',
        'Content-Type': 'application/json',
      });
    },
  );

  it('throws an ApiError with the code, the detail and the fresh state of a refused action', async () => {
    await signedIn();
    const fresh = {
      state: 'OPEN',
      headSha: 'f'.repeat(40),
      baseRef: 'main',
      isDraft: false,
      mergeable: 'MERGEABLE',
      mergeStateStatus: 'CLEAN',
      reviewDecision: null,
    };
    fetchMock.mockResolvedValueOnce(
      json(
        {
          type: '/problems/head_moved',
          title: 'Conflict',
          status: 409,
          code: 'head_moved',
          detail: 'moved',
          pr: fresh,
        },
        409,
        'application/problem+json',
      ),
    );
    const error = await client.approvePr('PR_1', { expectedHeadSha: sha }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 409, code: 'head_moved', detail: 'moved', pr: fresh });
  });

  it('keeps the permission the token is missing', async () => {
    await signedIn();
    fetchMock.mockResolvedValueOnce(
      json(
        {
          type: '/problems/missing_permission',
          title: 'Forbidden',
          status: 403,
          code: 'missing_permission',
          detail: 'The token needs the GitHub permission pull_requests: write',
        },
        403,
        'application/problem+json',
      ),
    );
    await expect(client.markPrReady('PR_1', { expectedHeadSha: sha })).rejects.toMatchObject({
      status: 403,
      code: 'missing_permission',
      detail: 'The token needs the GitHub permission pull_requests: write',
    });
  });
});
