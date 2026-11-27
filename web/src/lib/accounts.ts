import type { Accounts } from '@/api/client';

type Github = Accounts['github'];

/** How GitHub is connected. A personal access token is fine-grained when the server warned that it only sees what it was granted. */
export type Source = 'app' | 'oauth' | 'fine-grained' | 'classic';

export function sourceOf(github: Github): Source | null {
  if (!github.connected) return null;
  if (github.method === 'pat') return github.warning ? 'fine-grained' : 'classic';
  return github.method === 'oauth' ? 'oauth' : 'app';
}

/** The long name, as the accounts dialog shows it. */
export const SOURCE_NAME: Record<Source, string> = {
  app: 'GitHub App',
  oauth: 'OAuth',
  'fine-grained': 'Fine-grained token',
  classic: 'Classic token',
};

/** The short name of the badge in the repository list. */
export const SOURCE_SHORT: Record<Source, string> = {
  app: 'GitHub App',
  oauth: 'OAuth',
  'fine-grained': 'Token',
  classic: 'Token',
};

/** "Fetched via ..." in the repository dialog's note. */
export const SOURCE_VIA: Record<Source, string> = {
  app: 'the GitHub App',
  oauth: 'OAuth',
  'fine-grained': 'a fine-grained token',
  classic: 'a classic token',
};

/** Why the repository list says what it says: what the source can see. */
export const SOURCE_WHY: Record<Source, string> = {
  app: 'Fetched with the GitHub App, which sees the repositories it is installed on',
  oauth: 'Fetched with the OAuth sign-in',
  'fine-grained': 'Fetched with the token, which only sees the repositories it was granted',
  classic: 'Fetched with the token, which sees every repository you can',
};

/** The host of a Jira site, for "Jira · acme.atlassian.net". Null when the URL is not https. */
export function siteHost(url: string): string | null {
  try {
    const u = new URL(url);
    return u.protocol === 'https:' && u.host ? u.host : null;
  } catch {
    return null;
  }
}
