const SLUG = /^[a-z0-9][a-z0-9-]{0,99}$/;
const INSTALL = /^https:\/\/github\.com\/apps\/([a-z0-9][a-z0-9-]{0,99})\/installations\/new$/;

/** True for a GitHub App URL name: lowercase letters, digits and dashes. */
export function isAppSlug(slug: string): boolean {
  return SLUG.test(slug);
}

/**
 * The page where an org owner picks the repositories for the GitHub App. It is rebuilt from the
 * slug inside the URL the server sent, so anything that is not exactly that shape gives null.
 */
export function appInstallUrl(serverUrl: string | undefined): string | null {
  const slug = serverUrl === undefined ? undefined : INSTALL.exec(serverUrl)?.[1];
  return slug ? `https://github.com/apps/${slug}/installations/new` : null;
}
