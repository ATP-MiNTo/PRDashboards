export type Provider = 'github' | 'atlassian';

export type FinishResult =
  'ok' | 'denied' | 'invalid_state' | 'not_configured' | 'signed_out' | 'error';

export interface FinishParams {
  provider: Provider;
  code: string | null;
  state: string | null;
  error: string | null;
}

const PROVIDERS: readonly string[] = ['github', 'atlassian'];

export function isProvider(value: string | null): value is Provider {
  return value !== null && PROVIDERS.includes(value);
}

/** Reads provider, code, state and error from a query string. Null when the provider is unknown. */
export function parseFinishParams(search: string): FinishParams | null {
  const q = new URLSearchParams(search);
  const provider = q.get('provider');
  if (!isProvider(provider)) return null;
  return { provider, code: q.get('code'), state: q.get('state'), error: q.get('error') };
}

export interface FinishDeps {
  search: string;
  clearQuery: () => void;
  complete: (body: { provider: Provider; code: string; state: string }) => Promise<void>;
  errorCode: (error: unknown) => { status: number; code: string } | null;
  navigate: (url: string) => void;
}

export function doneUrl(provider: Provider | null, result: FinishResult): string {
  const q = new URLSearchParams({ result });
  if (provider) q.set('provider', provider);
  return `/auth/done/?${q.toString()}`;
}

/**
 * Finishes an OAuth authorization on the same origin. The query holds the code and state, so it is
 * cleared before anything else runs, and the code is sent only in a POST body.
 */
export async function runFinish(deps: FinishDeps): Promise<FinishResult> {
  const params = parseFinishParams(deps.search);
  deps.clearQuery();
  let result: FinishResult;
  if (!params) {
    result = 'error';
  } else if (params.error !== null) {
    result = params.error === 'access_denied' ? 'denied' : 'error';
  } else if (!params.code || !params.state) {
    result = 'error';
  } else {
    result = await complete(deps, {
      provider: params.provider,
      code: params.code,
      state: params.state,
    });
  }
  deps.navigate(doneUrl(params?.provider ?? null, result));
  return result;
}

async function complete(
  deps: FinishDeps,
  body: { provider: Provider; code: string; state: string },
): Promise<FinishResult> {
  try {
    await deps.complete(body);
    return 'ok';
  } catch (error) {
    const problem = deps.errorCode(error);
    if (problem?.status === 401) return 'signed_out';
    if (problem?.code === 'invalid_oauth_state') return 'invalid_state';
    if (problem?.code === 'provider_not_configured') return 'not_configured';
    return 'error';
  }
}
