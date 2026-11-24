import { describe, expect, it, vi } from 'vitest';
import { doneUrl, parseFinishParams, runFinish, type FinishDeps } from './oauthFinish';

function deps(over: Partial<FinishDeps> = {}): FinishDeps & {
  order: string[];
} {
  const order: string[] = [];
  return {
    order,
    search: '?provider=github&code=abc&state=xyz',
    clearQuery: vi.fn(() => void order.push('clear')),
    complete: vi.fn(async () => void order.push('complete')),
    errorCode: () => null,
    navigate: vi.fn(() => void order.push('navigate')),
    ...over,
  };
}

describe('parseFinishParams', () => {
  it('reads the query', () => {
    expect(parseFinishParams('?provider=atlassian&code=c&state=s')).toEqual({
      provider: 'atlassian',
      code: 'c',
      state: 's',
      error: null,
    });
  });

  it('rejects an unknown provider', () => {
    expect(parseFinishParams('?provider=gitlab&code=c&state=s')).toBeNull();
    expect(parseFinishParams('')).toBeNull();
  });
});

describe('doneUrl', () => {
  it('builds the done page URL from validated parts', () => {
    expect(doneUrl('github', 'ok')).toBe('/auth/done/?result=ok&provider=github');
    expect(doneUrl(null, 'error')).toBe('/auth/done/?result=error');
  });
});

describe('runFinish', () => {
  it('clears the query before posting the code, then goes to the done page', async () => {
    const d = deps();
    await expect(runFinish(d)).resolves.toBe('ok');
    expect(d.order).toEqual(['clear', 'complete', 'navigate']);
    expect(d.complete).toHaveBeenCalledWith({ provider: 'github', code: 'abc', state: 'xyz' });
    expect(d.navigate).toHaveBeenCalledWith('/auth/done/?result=ok&provider=github');
  });

  it('never puts the code or state in the next URL', async () => {
    const d = deps();
    await runFinish(d);
    expect(vi.mocked(d.navigate).mock.calls[0]![0]).not.toMatch(/abc|xyz/);
  });

  it('reports a denied authorization without calling the server', async () => {
    const d = deps({ search: '?provider=github&error=access_denied' });
    await expect(runFinish(d)).resolves.toBe('denied');
    expect(d.complete).not.toHaveBeenCalled();
  });

  it.each([
    '?provider=github&error=invalid_callback',
    '?provider=github',
    '?provider=github&code=c',
    '?code=c&state=s',
  ])('is an error for %s', async (search) => {
    const d = deps({ search });
    await expect(runFinish(d)).resolves.toBe('error');
    expect(d.complete).not.toHaveBeenCalled();
    expect(d.clearQuery).toHaveBeenCalled();
  });

  it.each([
    [{ status: 400, code: 'invalid_oauth_state' }, 'invalid_state'],
    [{ status: 409, code: 'provider_not_configured' }, 'not_configured'],
    [{ status: 401, code: 'unauthenticated' }, 'signed_out'],
    [{ status: 502, code: 'oauth_exchange_failed' }, 'error'],
    [null, 'error'],
  ] as const)('maps %j to %s', async (problem, expected) => {
    const d = deps({
      complete: vi.fn().mockRejectedValue(new Error('x')),
      errorCode: () => problem,
    });
    await expect(runFinish(d)).resolves.toBe(expected);
    expect(d.navigate).toHaveBeenCalledWith(expect.stringContaining(`result=${expected}`));
  });
});
