import type { Accounts, GithubToken, Snapshot } from '@/api/client';

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

/** The host of a Jira site, for "Jira · acme.atlassian.net". Null when the URL is not https. */
export function siteHost(url: string): string | null {
  try {
    const u = new URL(url);
    return u.protocol === 'https:' && u.host ? u.host : null;
  } catch {
    return null;
  }
}

/** The most characters a token name can have (the contract's TokenName). */
export const TOKEN_NAME_MAX = 40;

/** The kind of one stored token: a personal access token is fine-grained when the server says so. */
export function kindOf(token: Pick<GithubToken, 'method' | 'fineGrained'>): Source {
  if (token.method === 'pat') return token.fineGrained ? 'fine-grained' : 'classic';
  return token.method === 'oauth' ? 'oauth' : 'app';
}

/** The kind in running text, as the repository hint says it ("fetched with Work (fine-grained token, minto)"). */
const KIND_IN_TEXT: Record<Source, string> = {
  app: 'GitHub App',
  oauth: 'OAuth',
  'fine-grained': 'fine-grained token',
  classic: 'classic token',
};

/** Why a token name cannot be used, or null: 1 to 40 characters after trimming, no control characters. */
export function tokenNameProblem(name: string): string | null {
  const t = name.trim();
  if (t === '') return 'Give the token a name.';
  if (t.length > TOKEN_NAME_MAX) return `Use at most ${TOKEN_NAME_MAX} characters.`;
  if (/[\u0000-\u001f\u007f]/.test(t)) return 'The name cannot have control characters.';
  return null;
}

/** What the snapshot says about the token a repository is read and written with. */
export interface RepoToken {
  tokenId?: string;
  tokenName?: string;
  viewerPermission?: NonNullable<Snapshot['prs'][number]['repo']['viewerPermission']>;
}

/** The token of each repository that has an open pull request in the snapshot, by repository id. */
export function repoTokensOf(s: Snapshot): Record<string, RepoToken> {
  const out: Record<string, RepoToken> = {};
  for (const { repo } of s.prs) {
    if (out[repo.id]) continue;
    const t: RepoToken = {};
    if (repo.tokenId) t.tokenId = repo.tokenId;
    if (repo.tokenName) t.tokenName = repo.tokenName;
    if (repo.viewerPermission) t.viewerPermission = repo.viewerPermission;
    out[repo.id] = t;
  }
  return out;
}

/** The token of a repository, looked up in the list (the id first, then the name) so a rename shows at once. Null for a snapshot that predates tokens. */
export function tokenOfRepo(
  rt: RepoToken | undefined,
  tokens: readonly GithubToken[],
): { name: string; kind: Source | null; login: string | null } | null {
  if (!rt || (!rt.tokenId && !rt.tokenName)) return null;
  const lower = rt.tokenName?.toLowerCase();
  const found =
    tokens.find((t) => rt.tokenId !== undefined && t.id === rt.tokenId) ??
    tokens.find((t) => lower !== undefined && t.name.toLowerCase() === lower);
  const name = found?.name ?? rt.tokenName;
  if (!name) return null;
  return { name, kind: found ? kindOf(found) : null, login: found?.login ?? null };
}

const ROLE_NAME = {
  ADMIN: 'Admin',
  MAINTAIN: 'Maintain',
  WRITE: 'Write',
  TRIAGE: 'Triage',
  READ: 'Read',
} as const;

/** The badge of a repository: the token's name and its kind, as text, so two tokens differ by more than colour. */
export function tokenBadgeText(ref: { name: string; kind: Source | null }): string {
  return ref.kind ? `${ref.name} · ${SOURCE_NAME[ref.kind]}` : ref.name;
}

/**
 * The hint of a repository's source badge: which token fetches it, the account's role there and,
 * for a role that cannot write, what that blocks.
 */
export function repoTokenTip(
  ref: { name: string; kind: Source | null; login: string | null },
  role: RepoToken['viewerPermission'],
): string {
  const detail = [ref.kind ? KIND_IN_TEXT[ref.kind] : null, ref.login].filter(Boolean).join(', ');
  const parts = [`Fetched with ${ref.name}${detail ? ` (${detail})` : ''}.`];
  if (role) parts.push(`Your role here: ${ROLE_NAME[role]}.`);
  if (role === 'READ' || role === 'TRIAGE') {
    parts.push(
      "Read-only: Merge and Ready are not available here. Add a token from this repository's owner in Accounts.",
    );
  }
  if (ref.kind === 'fine-grained') parts.push('A fine-grained token works for one owner only.');
  return parts.join(' ');
}
