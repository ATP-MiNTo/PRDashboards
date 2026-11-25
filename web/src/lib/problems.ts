const MESSAGES: Record<string, string> = {
  invalid_token: 'GitHub rejected the token.',
  insufficient_scopes: 'The token lacks scopes the app needs (repo and read:org).',
  org_blocked: 'An organization blocked access. Authorize the token for it in GitHub.',
  upstream_error: 'The provider could not be reached. Try again.',
  not_connected: 'Not connected.',
  unknown_site: 'That Jira site is not available for the connected account.',
  provider_not_configured: 'Set up the GitHub App or the Atlassian app first.',
  rate_limited: 'Too many requests. Wait a moment and try again.',
  invalid_body: 'The values were not accepted.',
  head_moved:
    'The pull request changed since the page loaded. The row was refreshed. Review it and merge again.',
  not_mergeable: 'GitHub says this pull request cannot be merged right now.',
  stack_base_open: 'The pull request below it in the stack is still open. Merge that one first.',
  draft: 'A draft pull request cannot be merged.',
  not_open: 'The pull request is no longer open.',
  not_found: 'That item no longer exists.',
  transition_unavailable: 'Jira no longer offers that move for the ticket.',
  method_not_allowed: 'The repository does not allow that merge method.',
  ticket_not_linked: 'A ticket in the request is not linked to this pull request.',
  missing_field: 'Jira needs a value for a required field.',
  invalid_field: 'Jira did not accept a field value.',
  idempotency_key_reused: 'That merge request was already used. Close the panel and open it again.',
  unauthenticated: 'Your session ended. Open the app with scripts/start.sh',
};

/** Turns a problem code into text for the person. The code itself is never shown. */
export function describeProblem(code: string | undefined): string {
  return (code && MESSAGES[code]) || 'Something went wrong. Try again.';
}
