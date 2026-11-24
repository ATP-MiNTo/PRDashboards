const TOKEN = /^[A-Za-z0-9_-]{1,128}$/;

/** Reads the launch token from a location hash such as `#t=<token>`. */
export function parseLaunchToken(hash: string): string | null {
  const token = new URLSearchParams(hash.replace(/^#/, '')).get('t');
  return token !== null && TOKEN.test(token) ? token : null;
}

export type LaunchOutcome = 'signed-in' | 'missing-token' | 'rejected' | 'failed';

export interface LaunchDeps {
  hash: string;
  clearHash: () => void;
  createSession: (token: string) => Promise<void>;
  isRejection: (error: unknown) => boolean;
  goHome: () => void;
}

/** Exchanges the fragment token for a session. The fragment is cleared before any request. */
export async function runLaunch(deps: LaunchDeps): Promise<LaunchOutcome> {
  const token = parseLaunchToken(deps.hash);
  deps.clearHash();
  if (token === null) return 'missing-token';
  try {
    await deps.createSession(token);
  } catch (error) {
    return deps.isRejection(error) ? 'rejected' : 'failed';
  }
  deps.goHome();
  return 'signed-in';
}
