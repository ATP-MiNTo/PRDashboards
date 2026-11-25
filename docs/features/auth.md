# GitHub and Jira auth

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 5, 8.6 and 10.4. Related: [storage.md](storage.md), [security.md](security.md), [app-shell.md](app-shell.md).

## Purpose

Connects the app to GitHub (OAuth App, or a pasted personal access token) and to Jira (Atlassian
OAuth 2.0 3LO), keeps the Jira token fresh, and shows the state in a "Connected accounts" panel.
PR and ticket data are fetched by the poller ([poller.md](poller.md)).

Differences from the spec:

- The OAuth callback is cookie-less and redirects to a static finish page, which completes the flow
  with a same-origin `POST`. The spec's "only when the request carries the session cookie" cannot
  work because `SameSite=Strict` cookies are not sent on the cross-site redirect from the provider.
  See Key flows 4 and Rules.
- The "disconnected" state after a failed Jira refresh is `needsReconnect` on the stored token. The
  access token is dropped, the site list is kept.
- Setup is under `/setup`. The PAT form is on the same page. The Connected accounts panel is also
  in the settings drawer ([settings.md](settings.md)).

## Data model

- Secrets: `github.oauth.client`, `atlassian.oauth.client`, `github.token`, `atlassian.token` (see
  [storage.md](storage.md)).
- SQLite: `oauth_states` (pending flows) and the setting `jiraSiteId`.
- API schemas: `SetupState`, `ClientCredentials`, `PatRequest`, `Accounts` (`github`: connected,
  login, method, warning; `jira`: connected, needsReconnect, sites, selectedSiteId),
  `OAuthCompleteRequest`. Secrets and tokens appear in none of them.
- Problem codes added: `unknown_site`, `invalid_token`, `insufficient_scopes`, `org_blocked`,
  `upstream_error`, `not_connected`, `provider_not_configured`, `invalid_oauth_state`,
  `oauth_exchange_failed`.

| Method and path | Purpose |
| --- | --- |
| `GET /api/setup` | Which providers have client credentials, and the callback URLs |
| `PUT /api/setup/{github\|atlassian}` | Save client id and secret (write only) |
| `PUT /api/github/pat` | Validate and store a personal access token |
| `GET /api/accounts` | Connection state of both providers |
| `DELETE /api/accounts/{provider}` | Remove the stored token |
| `POST /api/accounts/{provider}/check` | Check the connection now (Viewer, or a Jira token refresh if due) |
| `GET /oauth/{provider}/start` | Protected. `302` to the provider, or `303` to `/auth/done/` with `not_configured` |
| `GET /oauth/{provider}/callback` | Cookie-less. `303` to `/oauth/finish/` |
| `POST /api/oauth/complete` | Protected. Finish the flow with `{provider, code, state}` |

## Code map

Server:

- [server/internal/auth/oauth.go](../../server/internal/auth/oauth.go): `OAuth`, `Endpoints`, `Start`, `Complete`, `Disconnect`, HTTP helpers, `accessibleSites`.
- [server/internal/auth/github.go](../../server/internal/auth/github.go): `completeGitHub`, `SetPAT`, `ScopeError`, `FineGrainedWarning`, `GitHubAccount`, `CheckGitHub`.
- [server/internal/auth/atlassian.go](../../server/internal/auth/atlassian.go): `completeAtlassian`, `AtlassianAccess`, `refresh`, `AtlassianAccount`, `CheckAtlassian`.
- [server/internal/auth/tokens.go](../../server/internal/auth/tokens.go): token blob types, `validSite`, errors.
- [server/internal/github/github.go](../../server/internal/github/github.go): `Client.Viewer`, `HasScope`, `OrgBlocked`, `Classify`, SSO header parsing.
- [server/internal/netguard/netguard.go](../../server/internal/netguard/netguard.go): `Guard`, the only HTTP client the providers use. [allow_e2e.go](../../server/internal/netguard/allow_e2e.go) adds loopback hosts in the e2e build only.
- [server/internal/backoff/backoff.go](../../server/internal/backoff/backoff.go): `Policy`, `Do`, `Retryable`, `Hint`, `Delay`.
- [server/internal/api/handlers_oauth.go](../../server/internal/api/handlers_oauth.go): `oauthStart`, `oauthCallback`, `CompleteOAuth`.
- [server/internal/api/handlers_accounts.go](../../server/internal/api/handlers_accounts.go): setup, PAT, accounts, check, disconnect.
- [server/internal/api/problem.go](../../server/internal/api/problem.go): `problemError` and its mapping to problem+json.
- [server/internal/fake/](../../server/internal/fake/fake.go): fake GitHub and Atlassian (build tag `e2e`).
- [server/cmd/prdash/fake_e2e.go](../../server/cmd/prdash/fake_e2e.go): `--fake` flag and `startFakes` (build tag `e2e`).

Web:

- [web/src/app/setup/page.tsx](../../web/src/app/setup/page.tsx): `/setup`, with [ClientForm](../../web/src/components/ClientForm.tsx) and [PatForm](../../web/src/components/PatForm.tsx).
- [web/src/app/oauth/finish/page.tsx](../../web/src/app/oauth/finish/page.tsx) and [web/src/lib/oauthFinish.ts](../../web/src/lib/oauthFinish.ts): clear the query, post to `/api/oauth/complete`, go to `/auth/done/`.
- [web/src/app/auth/done/page.tsx](../../web/src/app/auth/done/page.tsx): result message and a link back to `/accounts/`.
- [web/src/components/AccountsPanel.tsx](../../web/src/components/AccountsPanel.tsx): Connected accounts on `/accounts/`.
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
     stored as `github.token` with method `oauth`.
   - Atlassian: token exchange, then `accessible-resources`. Sites that are not `https://*.atlassian.net`
     are dropped. With one site it is selected, with several the person picks in the panel, which
     saves `jiraSiteId` through `PUT /api/settings`.
7. PAT: `PUT /api/github/pat` calls `Viewer` with the token. A classic token needs `repo` and
   `read:org` (else `insufficient_scopes`, nothing is stored). A fine-grained token is stored with
   `FineGrainedWarning`. A rejected token is `invalid_token`. `org_blocked` is returned when the
   response carries SSO enforcement.
8. Jira refresh (`AtlassianAccess`): under `tokenMu`, reload the blob and return the access token
   when it has more than one minute left. Otherwise `POST` a refresh, store the rotated refresh token
   and the new expiry, then return the access token. On a `400` or `401`, or a `403` `invalid_grant`,
   set `needsReconnect`, drop the access token and return `ErrNeedsReconnect`. Other failures keep
   the stored token and return an upstream error.
9. `POST /api/accounts/{provider}/check` runs `CheckGitHub` (`Viewer`) or `CheckAtlassian`
   (`AtlassianAccess`), so the panel can show a broken connection now.
10. `DELETE /api/accounts/{provider}` removes the token item. Client credentials stay.

## Outside calls

Only on a person's action, never in the background. All go through `netguard` (HTTPS, TLS 1.2
minimum, hosts `github.com`, `api.github.com`, `auth.atlassian.com`, `api.atlassian.com`).

- GitHub: token exchange (once per sign-in), GraphQL `viewer { login }` (once per sign-in, PAT
  entry or Check).
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
- Every refresh of the Atlassian token runs under `tokenMu`, and the rotated refresh token is
  stored before the new access token is used. Two parallel callers never spend one refresh token
  twice.
- Only `internal/github` and `internal/auth` make outbound calls, only through `netguard`.
  Redirects are not followed to hosts outside the allowlist. Site URLs from `accessible-resources`
  are validated before they are stored.
- The fakes and `--fake` exist only with the `e2e` build tag. In fake mode secrets are in memory,
  client credentials are pre-filled, and the guard allows `127.0.0.1:4411` and `127.0.0.1:4412`.
- Test hooks of the fakes: `GET /__admin/state`, `POST /__admin/config` and `POST /__admin/reset` on
  each fake. GitHub config: `deny`, `tamperState`, `sso`. Atlassian config: `deny`, `tamperState`,
  `expiresIn`, `refreshFail`, `sites` (1 or 2). Fake PATs: `ghp_classic_ok`, `ghp_classic_no_org`,
  `github_pat_finegrained`.

## Tests

- [server/internal/auth/oauth_test.go](../../server/internal/auth/oauth_test.go): start, state binding, expiry and replay, PKCE, both exchanges, site filtering, refresh rotation under concurrency, `needsReconnect`, disconnect.
- [server/internal/auth/persist_test.go](../../server/internal/auth/persist_test.go): token blobs in the secrets store.
- [server/internal/github/github_test.go](../../server/internal/github/github_test.go): `Viewer`, scopes, `OrgBlocked` and `Classify` against fixtures in [testdata/](../../server/internal/github/testdata).
- [server/internal/netguard/netguard_test.go](../../server/internal/netguard/netguard_test.go) and [allow_e2e_test.go](../../server/internal/netguard/allow_e2e_test.go): allowlist, TLS minimum, concurrency cap, timeouts, loopback only in the e2e build.
- [server/internal/backoff/backoff_test.go](../../server/internal/backoff/backoff_test.go): delays, `Retry-After`, jitter bounds, attempts.
- [server/internal/api/auth_api_test.go](../../server/internal/api/auth_api_test.go), [oauth_api_test.go](../../server/internal/api/oauth_api_test.go), [providers_test.go](../../server/internal/api/providers_test.go): setup, PAT, accounts, the callback and complete flow over HTTP, with fake providers.
- [server/cmd/prdash/fake_e2e_test.go](../../server/cmd/prdash/fake_e2e_test.go) and [fake_prod_test.go](../../server/cmd/prdash/fake_prod_test.go): `--fake` exists only with the e2e tag.
- Web: [AccountsPanel.test.tsx](../../web/src/components/AccountsPanel.test.tsx), [setup/page.test.tsx](../../web/src/app/setup/page.test.tsx), [auth/done/page.test.tsx](../../web/src/app/auth/done/page.test.tsx), [oauth/finish/page.test.tsx](../../web/src/app/oauth/finish/page.test.tsx), [oauthFinish.test.ts](../../web/src/lib/oauthFinish.test.ts), [problems.test.ts](../../web/src/lib/problems.test.ts).
- Playwright (`pnpm e2e`): [e2e/github.spec.ts](../../e2e/github.spec.ts) (cross-site OAuth sign-in, tampered state, replay, denial, PAT classic, missing scope, fine-grained, rejected) and [e2e/atlassian.spec.ts](../../e2e/atlassian.spec.ts), plus [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts) for the SSO-blocked dashboard (sign-in, two-site pick, refresh with rotation, refresh failure and reconnect, denial).
