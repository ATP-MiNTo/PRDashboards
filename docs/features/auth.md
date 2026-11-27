# GitHub and Jira auth

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 5, 8.6 and 10.4. Related: [github-tokens.md](github-tokens.md), [storage.md](storage.md), [security.md](security.md), [app-shell.md](app-shell.md).

## Purpose

Connects the app to GitHub (a GitHub App with user access tokens, and pasted personal access tokens, up to 10 tokens in all, see [github-tokens.md](github-tokens.md))
and to Jira (Atlassian OAuth 2.0 3LO), keeps the Jira and GitHub App tokens fresh, and shows the state
in the Accounts dialog of the dashboard and on the `/accounts/` page. A GitHub App is used because an org admin can install it on selected
repositories only, which an OAuth App with the `repo` scope cannot do. The token only sees the
repositories where the App is installed and the person has access.
PR and ticket data are fetched by the poller ([poller.md](poller.md)).

Differences from the spec:

- The OAuth callback is cookie-less and redirects to a static finish page, which completes the flow
  with a same-origin `POST`. The spec's "only when the request carries the session cookie" cannot
  work because `SameSite=Strict` cookies are not sent on the cross-site redirect from the provider.
  See Key flows 4 and Rules.
- The "disconnected" state after a failed Jira or GitHub App refresh is `needsReconnect` on the stored
  token. The Jira site list is kept. A GitHub token that needs a reconnect is kept until the person
  signs in again, and the flag is on that token only. A personal access token that GitHub rejected is flagged
  but still tried, so it recovers by itself when it works again.
- No private key, app ID or JWT: only user access tokens are used. GitHub ignores `scope` for a
  GitHub App, so none is sent. Permissions come from the App's settings (README, First-time setup). Approving, requesting changes and marking a draft ready need Pull requests: write on a fine-grained token or the App (a classic token needs `repo`); without it the API answers `missing_permission` naming the repository and `pull_requests: write`, and that a fine-grained token (or a GitHub App installation) works for one owner only, so a repository of another owner needs a token from that owner ([review-actions.md](review-actions.md)).
- Setup is under `/setup`. The PAT form is on the same page. The dashboard has the Accounts dialog
  ([AccountsModal.tsx](../../web/src/components/Accounts/AccountsModal.tsx)), opened by "Manage accounts" at the bottom
  of the sidebar ([sidebar-shell.md](sidebar-shell.md)). The old right-hand Account side panel is gone. The `/accounts/`
  page stays for the returns of the OAuth flows and for the e2e specs, with the older Connected accounts content.

## Data model

- Secrets: `github.oauth.client` (client id, secret and the optional `appSlug`), `atlassian.oauth.client`, `github.tokens`, `atlassian.token` (see
  [storage.md](storage.md)). They live in the encrypted vault `vault.enc`, not in the OS keychain: the
  Atlassian token is too big for the macOS keychain, which holds only the vault key.
- SQLite: `oauth_states` (pending flows) and the setting `jiraSiteId`.
- `github.tokens` is a JSON list of up to 10 entries, the first one the primary ([github-tokens.md](github-tokens.md)). The former single `github.token` is moved into it (one entry, named by default) at start and on the first read. An entry holds `id`, `name`, `fineGrained`, `token`, `method` (`app` for a new sign-in, `pat`, or the legacy `oauth`, which
  keeps working as stored), `login`, `warning`, `refreshToken`, `expiresAt` (zero or absent means the
  token does not expire: PATs, legacy tokens, Apps with expiry off) and `needsReconnect`.
- API schemas: `Setup` and `ProviderSetup` (`configured`, `callbackUrl`, and `installUrl` for GitHub
  when a slug is saved), `ClientCredentials` (with the optional `appSlug`, pattern
  `^[a-z0-9][a-z0-9-]{0,99}$`), `PatRequest` (with an optional `name`), `Accounts` (`github`: the summary, connected when any token is stored,
  login, method (`app`, `oauth`, `pat`) and warning of the primary token, needsReconnect when any token needs it; `githubTokens`: the list of `GithubToken` (id, name, method, fineGrained, login, warning, needsReconnect) and `githubTokenLimit`; `jira`: connected, needsReconnect, sites, selectedSiteId),
  `OAuthCompleteRequest`. Secrets and tokens appear in none of them.
- Problem codes added: `unknown_site`, `invalid_token`, `insufficient_scopes`, `org_blocked`,
  `upstream_error`, `not_connected`, `provider_not_configured`, `invalid_oauth_state`,
  `oauth_exchange_failed`, `token_limit`, `token_duplicate`, `token_name_taken`.

| Method and path | Purpose |
| --- | --- |
| `GET /api/setup` | Which providers have client credentials, the callback URLs, and the GitHub install link |
| `PUT /api/setup/{github\|atlassian}` | Save client id, secret and the optional GitHub App slug (write only) |
| `PUT /api/github/pat` | Validate and store a personal access token: the first one, or one more when tokens exist |
| `POST /api/github/tokens`, `PUT` and `DELETE /api/github/tokens/{id}`, `POST /api/github/tokens/{id}/check` | Add, rename, remove and check one GitHub token ([github-tokens.md](github-tokens.md)) |
| `GET /api/accounts` | Connection state of both providers |
| `DELETE /api/accounts/{provider}` | Remove the stored tokens (every GitHub token) |
| `POST /api/accounts/{provider}/check` | Check the connection now (every GitHub token: a refresh if due, then Viewer; or a Jira token refresh if due) |
| `GET /oauth/{provider}/start` | Protected. `302` to the provider, or `303` to `/auth/done/` with `not_configured` |
| `GET /oauth/{provider}/callback` | Cookie-less. `303` to `/oauth/finish/` |
| `POST /api/oauth/complete` | Protected. Finish the flow with `{provider, code, state}` |

## Code map

Server:

- [server/internal/auth/oauth.go](../../server/internal/auth/oauth.go): `OAuth`, `Endpoints`, `Start`, `Complete`, `Disconnect`, `SetClient` (slug check), `InstallURL`, HTTP helpers, `accessibleSites`.
- [server/internal/auth/github.go](../../server/internal/auth/github.go): `completeGitHub`, `GitHubAccess`, `refreshGitHub`, `AddPAT`, `ScopeError`, `FineGrainedWarning`, `GitHubAccount` (the primary), `CheckGitHub`, `CheckGitHubToken`, `MarkGitHubRejected`. [github_tokens.go](../../server/internal/auth/github_tokens.go): the list, migration, names, limit ([github-tokens.md](github-tokens.md)).
- [server/internal/auth/atlassian.go](../../server/internal/auth/atlassian.go): `completeAtlassian`, `AtlassianAccess`, `refresh`, `AtlassianAccount`, `CheckAtlassian`.
- [server/internal/auth/tokens.go](../../server/internal/auth/tokens.go): token blob types, method constants, `ValidSlug`, `validSite`, errors.
- [server/internal/github/github.go](../../server/internal/github/github.go): `Client.Viewer`, `HasScope`, `OrgBlocked`, `PermissionError`, `Classify`, SSO header parsing. A FORBIDDEN that only says the token lacks a permission is a `PermissionError`, not an organization block ([merge-and-transition.md](merge-and-transition.md)).
- [server/internal/netguard/netguard.go](../../server/internal/netguard/netguard.go): `Guard`, the only HTTP client the providers use. [allow_e2e.go](../../server/internal/netguard/allow_e2e.go) adds loopback hosts in the e2e build only.
- [server/internal/backoff/backoff.go](../../server/internal/backoff/backoff.go): `Policy`, `Do`, `Retryable`, `Hint`, `Delay`.
- [server/internal/api/handlers_oauth.go](../../server/internal/api/handlers_oauth.go): `oauthStart`, `oauthCallback`, `CompleteOAuth`.
- [server/internal/api/handlers_accounts.go](../../server/internal/api/handlers_accounts.go): setup, accounts, check, disconnect. [handlers_tokens.go](../../server/internal/api/handlers_tokens.go): `PUT /api/github/pat` and the token routes.
- [server/internal/api/problem.go](../../server/internal/api/problem.go): `problemError` and its mapping to problem+json.
- [server/internal/fake/](../../server/internal/fake/fake.go): fake GitHub and Atlassian (build tag `e2e`).
- [server/cmd/prdash/fake_e2e.go](../../server/cmd/prdash/fake_e2e.go): `--fake` flag and `startFakes` (build tag `e2e`).

Web:

- [web/src/app/setup/page.tsx](../../web/src/app/setup/page.tsx): `/setup`, with [ClientForm](../../web/src/components/ClientForm.tsx) (the "GitHub App" card has the slug field and the install link) and [PatForm](../../web/src/components/PatForm.tsx).
- [web/src/lib/githubApp.ts](../../web/src/lib/githubApp.ts): `isAppSlug`, `appInstallUrl` (the install link is rebuilt from the validated slug in the server URL, otherwise none).
- [web/src/app/oauth/finish/page.tsx](../../web/src/app/oauth/finish/page.tsx) and [web/src/lib/oauthFinish.ts](../../web/src/lib/oauthFinish.ts): clear the query, post to `/api/oauth/complete`, go to `/auth/done/`.
- [web/src/app/auth/done/page.tsx](../../web/src/app/auth/done/page.tsx): result message and a link back to `/accounts/`.
- [web/src/components/AccountsPanel.tsx](../../web/src/components/AccountsPanel.tsx): Connected accounts, on `/accounts/` only.
- [web/src/components/Accounts/AccountsModal.tsx](../../web/src/components/Accounts/AccountsModal.tsx) and [AccountsModal.module.css](../../web/src/components/Accounts/AccountsModal.module.css): the Accounts dialog (a wide, centred [Modal](dialog-and-popover.md) with the shared Close button). The GitHub card is the token list "GitHub tokens" with a counter ("2 of 10", `Accounts.githubTokenLimit`) and the line that a fine-grained token works for one owner only, so one token per owner is needed. One row per `Accounts.githubTokens` entry (first is the Primary one, with an info tip): the name with "Rename" (an inline field with "Save name" and Cancel or Escape; 1 to 40 characters, `tokenNameProblem`; `PUT /api/github/tokens/{id}`, and `token_name_taken` is shown in the row), the kind (GitHub App, OAuth, Classic token or Fine-grained token, `kindOf`), the login, the server's warning, a Connected or Needs reconnect badge, and the buttons "Reconnect" (a link to `/oauth/github/start`, for the App and OAuth), "Check" (`POST /api/github/tokens/{id}/check`) and "Remove" (asks first; `DELETE /api/github/tokens/{id}`; for the only token it says that this disconnects GitHub). A personal access token that needs a reconnect says to remove it and add a new one. Below the list the "Add token" form (an optional name and a password field that never shows a stored token; `POST /api/github/tokens`) is disabled with "You can connect up to 10 tokens" at the limit; `token_limit`, `token_duplicate`, `token_name_taken`, `invalid_token`, `insufficient_scopes` and `org_blocked` read as plain sentences ([problems.ts](../../web/src/lib/problems.ts)). "Disconnect GitHub" (`DELETE /api/accounts/github`) removes every token. With no token the card shows "Connect GitHub" only; the first PAT is added on the Setup page. The table Repository / Source lists every repository of `GET /api/repos` with a badge whose text is the token name and kind ("Work · Fine-grained token"), found from the snapshot repository's `tokenId` and `tokenName`; a repository the snapshot has no token for shows the generic kind of the summary. The Jira card shows the site (a select when there are several, which saves `jiraSiteId`), "Reconnect" (`/oauth/atlassian/start`), "Check Jira" and "Disconnect Jira", or "Connect Jira"; an unconfigured provider shows "Set up" (a link to `/setup/`). The Browsers card has "Sign out everywhere" (`POST /api/sessions/revoke-all`, which also ends this session) and then says how to sign in again. See [github-tokens.md](github-tokens.md) for the several tokens.
- [web/src/api/queries.ts](../../web/src/api/queries.ts) and [providers.tsx](../../web/src/app/providers.tsx): TanStack Query hooks and client. [web/src/lib/problems.ts](../../web/src/lib/problems.ts): problem code to text.

E2e:

- [scripts/e2e.mjs](../../scripts/e2e.mjs), [e2e/playwright.config.ts](../../e2e/playwright.config.ts), [e2e/helpers.ts](../../e2e/helpers.ts), specs below.

## Key flows

1. Setup: `/setup` calls `PUT /api/setup/{provider}`. The credentials go to the secrets store and
   are never returned. `GET /api/setup` returns `configured` and the callback URL only.
2. Start: the "Connect" link goes to `/oauth/{provider}/start`. `OAuth.Start` creates a 256-bit
   state (and a PKCE verifier for GitHub), stores the state hashed with the session key and a
   10-minute expiry, and redirects `302` to the provider's authorize URL. Without client
   credentials it redirects `303` to `/auth/done/?provider=..&result=not_configured`.
3. The person authorizes at the provider, which redirects to
   `http://127.0.0.1:4317/oauth/{provider}/callback?code=..&state=..` (or `error=access_denied`).
4. `oauthCallback` has no session check. It validates only the shape of the parameters (`shaped`:
   `code` up to 2048, `state` up to 256, `error` up to 64 characters of a safe charset) and answers
   `303` to `/oauth/finish/?provider=..&code=..&state=..` (or `&error=..`) with `Cache-Control:
   no-store`. A bad shape goes to `/auth/done/` with `result=error`.
5. `/oauth/finish/` is a same-origin navigation, so the cookie is sent. Its script calls
   `history.replaceState` to drop the query, then `POST /api/oauth/complete`. A denied flow skips
   the POST. The page navigates to `/auth/done/?provider=..&result=ok|denied|invalid_state|signed_out|error`.
6. `OAuth.Complete`: `ConsumeOAuthState` (one `DELETE ... RETURNING`, must match the provider and
   the calling session and be unexpired). Then the code exchange:
   - GitHub: `POST` to the token endpoint with the verifier, then `Viewer` to learn the login,
     stored under `tokenMu` in `github.tokens` (`saveTokens`/`saveSignIn`: the App or OAuth entry is replaced in place, there is at most one, otherwise a new entry is added and the limit applies) with method `app`, the refresh token and
     `expiresAt = now + expires_in`. Without `expires_in` and `refresh_token` (expiry turned off in
     the App) the token never expires. The authorize URL has no `scope`.
   - Atlassian: token exchange, then `accessible-resources`. Sites that are not `https://*.atlassian.net`
     are dropped. With one site it is selected, with several the person picks in the panel, which
     saves `jiraSiteId` through `PUT /api/settings`.
7. PAT: `PUT /api/github/pat` (or `POST /api/github/tokens`) calls `Viewer` with the token, after the name, the limit and the duplicate checks. A classic token needs `repo` and
   `read:org` (else `insufficient_scopes`, nothing is stored). A fine-grained token is stored with
   `FineGrainedWarning`. A rejected token is `invalid_token`. `org_blocked` (SAML SSO or an organization token policy, `OrgBlocked.Reason` `sso` or `policy`) is returned when the
   response carries SSO enforcement.
8. Jira refresh (`AtlassianAccess`): under `tokenMu`, reload the blob and return the access token
   when it has more than one minute left. Otherwise `POST` a refresh, store the rotated refresh token
   and the new expiry, then return the access token. On a `400` or `401`, or a `403` `invalid_grant`,
   set `needsReconnect`, drop the access token and return `ErrNeedsReconnect`. Other failures keep
   the stored token and return an upstream error.
9. GitHub refresh (`GitHubAccess(ctx, id)`, per token, used by the poller, the merge and review flows and `CheckGitHub`; an empty id is the primary token, an unknown id is `ErrTokenNotFound`): mirrors
   `AtlassianAccess`. Fast path without the lock when the token has no expiry (PAT, legacy `oauth`,
   Apps with expiry off) or more than `RefreshSkew` left. Otherwise take `tokenMu`, reload, check
   again, then `POST` `grant_type=refresh_token` to the token endpoint with the client id and secret.
   The rotated refresh token and the new expiry are stored before the access token is returned. GitHub
   answers a bad refresh token with status `200` and `{"error":"bad_refresh_token"}`: that, or a `400`
   or `401`, sets `needsReconnect` on that entry only and returns `ErrNeedsReconnect`. Network errors, `5xx` and other
   error bodies keep the stored token and return an error. A flagged token returns `ErrNeedsReconnect`
   without calling GitHub. Signing in again replaces the token and clears the flag. The poller maps
   `ErrNeedsReconnect` to the GitHub source state `disconnected` with the text "Reconnect GitHub".
   The panel shows a "Reconnect GitHub" link to `/oauth/github/start`.
10. `POST /api/accounts/{provider}/check` runs `CheckGitHub` (`CheckGitHubToken` for each token: `GitHubAccess`, then `Viewer`) or
    `CheckAtlassian` (`AtlassianAccess`), so the panel can show a broken connection now. A token that
    needs a reconnect answers with the accounts instead of an error.
11. `DELETE /api/accounts/{provider}` removes the token items (all GitHub tokens: `github.tokens` and a legacy `github.token`). Client credentials stay.

## Outside calls

Only on a person's action, never in the background. All go through `netguard` (HTTPS, TLS 1.2
minimum, hosts `github.com`, `api.github.com`, `auth.atlassian.com`, `api.atlassian.com`).

- GitHub: token exchange (once per sign-in), token refresh (only when an expiring App token is
  within a minute of expiry, at most once per 8 hours of use, one at a time, so concurrent callers
  share one), GraphQL `viewer { login }` (once per sign-in, PAT entry or Check, per token). The install link is
  only a link in the UI, the server never calls it.
- Atlassian: token exchange and `accessible-resources` (once per sign-in), token refresh (at most
  once per hour of use, one at a time).
- Rate limits and `5xx` use `backoff.Do`: `Retry-After` first, else exponential backoff with
  jitter, a few attempts. `netguard` allows 4 concurrent requests per provider, a 10 s dial and a
  30 s client timeout.

## Rules and invariants

- The callback is the only unprotected route that takes input. It is `GET` only, on the two exact
  paths, reads three parameters of bounded size, never touches the database or a provider, and
  logs nothing from the query. The code reaches the server's exchange only through the protected,
  CSRF-checked `POST /api/oauth/complete`.
- A state is 256 random bits, stored hashed, bound to the session and the provider, valid for 10
  minutes and consumed on the first attempt, accepted or not. A tampered, replayed, expired or
  foreign state gives `invalid_oauth_state` and no exchange.
- `/oauth/{provider}/start` is protected, so a page on another site cannot start a flow for the
  person.
- Tokens, client secrets, codes and verifiers are never logged, stored in SQLite (except the
  pending verifier), returned by the API or sent to the browser. `GET /api/accounts` returns
  logins, methods, warnings and site names only.
- Every refresh of the Atlassian token and of a GitHub App token runs under `tokenMu`, and the
  rotated refresh token is stored before the new access token is used. Two parallel callers never
  spend one refresh token twice. Writes of `github.tokens` (sign-in, PAT, rename, remove, check, disconnect) take
  the same lock, and a refresh writes its own entry by id, so it never loses another token's change.
- The install link is built on the server only from a slug that matches `^[a-z0-9][a-z0-9-]{0,99}$`,
  and the web accepts only a link of exactly that shape on `github.com`.
- Only `internal/github` and `internal/auth` make outbound calls, only through `netguard`.
  Redirects are not followed to hosts outside the allowlist. Site URLs from `accessible-resources`
  are validated before they are stored.
- The fakes and `--fake` exist only with the `e2e` build tag. In fake mode secrets are in memory,
  client credentials are pre-filled, and the guard allows `127.0.0.1:4411` and `127.0.0.1:4412`.
- Test hooks of the fakes: `GET /__admin/state`, `POST /__admin/config` and `POST /__admin/reset` on
  each fake. GitHub config: `deny`, `tamperState`, `sso`, `expiresIn` (lifetime in seconds of user tokens,
  default 28800, 1 makes every use refresh), `refreshFail` (every refresh answers `bad_refresh_token`).
  The fake GitHub authorize endpoint rejects a request that carries `scope`, its token endpoint returns
  `refresh_token` and `expires_in` and supports `grant_type=refresh_token` with rotation (an unknown or
  used refresh token gets `bad_refresh_token`), and its user tokens carry no `X-OAuth-Scopes` header
  (fake PATs still do). Atlassian config: `deny`, `tamperState`,
  `expiresIn`, `refreshFail`, `sites` (1 or 2). Fake PATs: `ghp_classic_ok`, `ghp_classic_no_org`,
  `github_pat_finegrained`, and the two owners' tokens `github_pat_acme` and `github_pat_oss` ([github-tokens.md](github-tokens.md)). `GET /__admin/state` of the GitHub fake lists `calls`, each call with the token it used.

## Tests

- [server/internal/auth/oauth_test.go](../../server/internal/auth/oauth_test.go): start, state binding, expiry and replay, PKCE, both exchanges (no scope on the GitHub authorize URL), site filtering, Jira and GitHub refresh rotation under concurrency (one refresh call for 25 callers), `needsReconnect`, transient failures that keep the token, non-expiring and legacy `oauth` tokens, slug validation and `InstallURL`, disconnect.
- [server/internal/auth/github_tokens_test.go](../../server/internal/auth/github_tokens_test.go): the list: migration, add, rename, remove, limit, duplicates, per-token refresh, rejection ([github-tokens.md](github-tokens.md)).
- [server/internal/auth/persist_test.go](../../server/internal/auth/persist_test.go): token blobs in the secrets store.
- [server/internal/github/github_test.go](../../server/internal/github/github_test.go): `Viewer`, scopes, `OrgBlocked` and `Classify` against fixtures in [testdata/](../../server/internal/github/testdata).
- [server/internal/netguard/netguard_test.go](../../server/internal/netguard/netguard_test.go) and [allow_e2e_test.go](../../server/internal/netguard/allow_e2e_test.go): allowlist, TLS minimum, concurrency cap, timeouts, loopback only in the e2e build.
- [server/internal/backoff/backoff_test.go](../../server/internal/backoff/backoff_test.go): delays, `Retry-After`, jitter bounds, attempts.
- [server/internal/api/auth_api_test.go](../../server/internal/api/auth_api_test.go), [oauth_api_test.go](../../server/internal/api/oauth_api_test.go), [providers_test.go](../../server/internal/api/providers_test.go) and [tokens_api_test.go](../../server/internal/api/tokens_api_test.go): setup (including the slug and `installUrl`), PAT, the token routes, accounts (including GitHub `needsReconnect`), the callback and complete flow over HTTP, with fake providers.
- [server/cmd/prdash/fake_e2e_test.go](../../server/cmd/prdash/fake_e2e_test.go) and [fake_prod_test.go](../../server/cmd/prdash/fake_prod_test.go): `--fake` exists only with the e2e tag.
- Web: [AccountsModal.test.tsx](../../web/src/components/Accounts/AccountsModal.test.tsx) (the token list and counter, the add form and the limit, rename with `token_name_taken`, remove with its confirm, check, reconnect states, disconnect, the Jira site, connect and set up, reconnect states, sign out everywhere, closing), [accounts.test.ts](../../web/src/lib/accounts.test.ts), [AccountsPanel.test.tsx](../../web/src/components/AccountsPanel.test.tsx), [setup/page.test.tsx](../../web/src/app/setup/page.test.tsx), [auth/done/page.test.tsx](../../web/src/app/auth/done/page.test.tsx), [oauth/finish/page.test.tsx](../../web/src/app/oauth/finish/page.test.tsx), [oauthFinish.test.ts](../../web/src/lib/oauthFinish.test.ts), [problems.test.ts](../../web/src/lib/problems.test.ts), [githubApp.test.ts](../../web/src/lib/githubApp.test.ts) (slug and install link shape). The poller maps `ErrNeedsReconnect` in [poller_test.go](../../server/internal/poller/poller_test.go).
- Playwright (`pnpm e2e`): [e2e/github.spec.ts](../../e2e/github.spec.ts) (cross-site GitHub App sign-in, refresh with rotation, tampered state, replay, denial, PAT classic, missing scope, fine-grained, rejected) and [e2e/atlassian.spec.ts](../../e2e/atlassian.spec.ts), plus [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts) for the SSO-blocked dashboard (sign-in, two-site pick, refresh with rotation, refresh failure and reconnect, denial).
