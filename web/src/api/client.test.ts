import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
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
