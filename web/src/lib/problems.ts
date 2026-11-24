const MESSAGES: Record<string, string> = {
  invalid_token: 'GitHub rejected the token.',
  insufficient_scopes: 'The token lacks scopes the app needs (repo and read:org).',
  org_blocked: 'An organization blocked access. Authorize the token for it in GitHub.',
  upstream_error: 'The provider could not be reached. Try again.',
  not_connected: 'Not connected.',
  unknown_site: 'That Jira site is not available for the connected account.',
  provider_not_configured: 'Set up the OAuth app first.',
  rate_limited: 'Too many requests. Wait a moment and try again.',
  invalid_body: 'The values were not accepted.',
  unauthenticated: 'Your session ended. Open the app with scripts/start.sh',
};

/** Turns a problem code into text for the person. The code itself is never shown. */
export function describeProblem(code: string | undefined): string {
  return (code && MESSAGES[code]) || 'Something went wrong. Try again.';
}
