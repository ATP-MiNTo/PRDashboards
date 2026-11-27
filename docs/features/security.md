# Security middleware

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 8.1 to 8.5. Related: [review-actions.md](review-actions.md), [app-shell.md](app-shell.md), [storage.md](storage.md), [auth.md](auth.md).

## Purpose

Treats the browser as a remote client. Every request passes one middleware chain before it reaches
a handler, and every response carries the same security headers. Launch tokens and session cookies
are how the browser proves it was started by the person who ran the binary.

Differences from the spec:

- Sessions are persisted in SQLite (hashed ids) behind an in-memory cache, and
  `POST /api/sessions/revoke-all` ends them all ([storage.md](storage.md)).
- The cookie signing key is the secrets item `session.key` (32 random bytes). An older
  `<data-dir>/session.key` file is imported once and deleted.
- Two routes are reachable without a session: the static pages and `GET /oauth/{provider}/callback`
  (cookie-less by design, see [auth.md](auth.md)). `GET /oauth/{provider}/start` is protected.
- The session cookie has no `Secure` flag. The app is served over plain `http` on loopback, and a
  `Secure` cookie is not sent back on `http://127.0.0.1`. `HttpOnly` and `SameSite=Strict` stay.
- `GET /events` (server-sent events, [poller.md](poller.md)) is guarded like `/api/*`: the same chain runs, so it needs a session. It sets `Cache-Control: no-store` and the hub sets a write deadline per write.

## Data model

- `prd_session` cookie: `base64url(sessionID) + "." + base64url(HMAC-SHA256(key, sessionID))`.
  Session id is 32 random bytes. `HttpOnly`, `SameSite=Strict`, `Path=/`, `Max-Age` 30 days.
- Session record (SQLite `sessions` table, cached in memory): key is `sha256(sessionID)`, value is
  `created`, `lastIssued`, `expires`. The cookie is sent again when `lastIssued` is older than 24 h, and `expires` slides to
  30 days from the last issue.
- CSRF token: `base64url(HMAC-SHA256(key, "csrf:" + sessionID))`. Returned by `GET /api/session`,
  sent back as `X-PRD-CSRF` on every non-GET request.
- Launch token: 32 random bytes, base64url. Valid for 2 minutes, single use. The newest token is
  in `<data-dir>/launch.token` (mode 0600, written through a temporary file and a rename) as JSON
  `{"port":N,"token":"..."}`.
- Problem body: `{type, title, status, code, detail?}` with content type
  `application/problem+json`.

| Status | Code | Raised by |
| --- | --- | --- |
| 421 | `misdirected_host` | Host not `127.0.0.1:<port>` or `localhost:<port>` |
| 405 | `cors_not_allowed` | any `OPTIONS` request |
| 403 | `cross_site` | `Sec-Fetch-Site` present and not `same-origin` on `/api/*` |
| 403 | `bad_origin` | non-GET `/api/*` with an `Origin` outside the allowlist |
| 429 | `rate_limited` | token bucket empty (`Retry-After` in seconds) |
| 401 | `unauthenticated` | no valid session cookie on `/api/*` |
| 403 | `bad_csrf` | `X-PRD-CSRF` missing or wrong on a non-GET |
| 415 | `unsupported_media_type` | write with a body whose type is not `application/json` |
| 413 | `body_too_large` | body over 64 KiB |
| 400 | `invalid_body` | not one JSON value, or fails the OpenAPI schema (unknown fields included) |
| 400 | `invalid_request` | a GET that fails the OpenAPI schema |
| 404 | `not_found` | unknown `/api/*` path, after authentication |
| 500 | `internal_error` | handler error or panic, details only in the log |
| 401 | `invalid_launch_token` | `POST /api/session` with an unknown, expired or spent token |
| 400 | `invalid_oauth_state` | `POST /api/oauth/complete` with an unknown, spent, expired or foreign state |
| 502 | `oauth_exchange_failed`, `upstream_error` | provider rejected the code, or could not be reached |
| 422 | `invalid_token`, `insufficient_scopes`, `unknown_site` | PAT rejected or short of scopes, site not reachable with the account |
| 403 | `org_blocked` | PAT validation hit an SSO-enforcing organization |
| 409 | `not_connected`, `provider_not_configured` | account operation without a connection, or without client credentials |
| 409 | `not_open`, `head_moved`, `not_draft`, `own_pull_request` | review action refused from fresh GitHub data ([review-actions.md](review-actions.md)) |
| 422 | `body_required`, `review_rejected` | request changes without a text, or GitHub refused the review |
| 403 | `missing_permission` | the token lacks a GitHub permission (a review needs `pull_requests: write`) |

## Code map

Server:

- [server/internal/api/mw/problem.go](../../server/internal/api/mw/problem.go): problem writer, problem codes, `Protected` (includes `/oauth/{provider}/start`, excludes the callback), `Chain`.
- [server/internal/api/mw/guards.go](../../server/internal/api/mw/guards.go): `HostAllowlist`, `NoCORS`, `SameOrigin` (Sec-Fetch-Site and Origin).
- [server/internal/api/mw/ratelimit.go](../../server/internal/api/mw/ratelimit.go): token buckets, 30 writes per minute per session, 120 rule previews per minute per session and 10 per minute for session creation, with idle buckets swept.
- [server/internal/api/mw/session.go](../../server/internal/api/mw/session.go): `Identify`, `RequireSession`, `RequireCSRF`.
- [server/internal/api/mw/body.go](../../server/internal/api/mw/body.go): content type, size, single JSON value, OpenAPI request validation (`kin-openapi`, with the spec embedded by the generator).
- [server/internal/api/mw/headers.go](../../server/internal/api/mw/headers.go): `CSP`, `LoadScriptHashes`, `SecurityHeaders`.
- [server/internal/api/mw/observe.go](../../server/internal/api/mw/observe.go): `AccessLog` and `Recover`.
- [server/internal/api/server.go](../../server/internal/api/server.go): `New` assembles the chain, and the `CreateSession` and `GetSession` handlers.
- [server/internal/auth/launch.go](../../server/internal/auth/launch.go): `Launcher` (issue, rotate, consume, remove, `ReadLaunchFile`).
- [server/internal/auth/session.go](../../server/internal/auth/session.go): `Sessions` (write-through cache over the store, `Load`, `RevokeAll`, `ExpiredCookie`), cookie and CSRF helpers, `LoadOrCreateKey`.
- [server/internal/logging/redact.go](../../server/internal/logging/redact.go): redacting `slog` handler.
- [api/openapi.yaml](../../api/openapi.yaml): contract. Generated Go code is in `server/internal/api/gen`, TS types in `web/src/api/schema.d.ts`.

Web:

- [web/src/api/client.ts](../../web/src/api/client.ts): typed fetch client, always `credentials: 'same-origin'`, sends `X-PRD-CSRF` on non-GET.
- [web/scripts/csp-hashes.mjs](../../web/scripts/csp-hashes.mjs): writes `csp.json` with the hashes of inline scripts after the build, and fails the build on inline styles.

## Key flows

1. Every request: `AccessLog` > `Recover` > `SecurityHeaders` > `HostAllowlist` > `NoCORS` >
   `SameOrigin` > `Identify` > `RateLimit` > `RequireSession` > `RequireCSRF` > `Validate` > router.
   `SecurityHeaders` runs before any check, so rejections carry the headers too.
2. `Identify` reads `prd_session`, verifies the HMAC in constant time and looks the id hash up. A
   valid session goes into the request context and the cookie is sent again when older than 24 h.
   It never rejects.
3. `RateLimit` applies to non-GET `/api/*`. The key is the session, or one shared bucket without a
   session. `POST /api/session` uses its own bucket (10 per minute). The two rule preview endpoints
   (`POST /api/rules/preview` and `/api/rules/previews`) are read-only computations over cached
   data with no outside call, so they do not spend the write bucket of 30 per minute: they have a
   separate bucket of 120 per minute per session, and still pass Host, Sec-Fetch-Site, Origin,
   session, CSRF, body size and schema checks. A bucket idle for a full minute (a full bucket again)
   is dropped, swept at most once a minute while requests arrive, so the maps do not grow with
   sessions that went away.
4. `Validate` reads the body (64 KiB cap), requires a single JSON value, then runs
   `openapi3filter.ValidateRequest`. Paths missing from the spec pass to the router, which answers
   `404 not_found`. A write without a body skips the content type and JSON checks, and the schema
   still rejects it when the body is required.
5. Launch: `Launcher.Consume` compares the presented token with the live tokens in constant time,
   spends it and issues a new one. `CreateSession` then calls `Sessions.Create` and sets the cookie.
6. Startup refuses to run when the data dir is group- or world-writable, or when `session.key`,
   `secrets.enc`, `vault.enc` or `data.db` is accessible to group or others, and the lock is taken before the
   port is bound (see [app-shell.md](app-shell.md)).
7. `POST /api/sessions/revoke-all` clears the cache, deletes every session row and answers `204`
   with an expired cookie.

## Outside calls

None.

## Rules and invariants

- No `Access-Control-*` header is ever sent. `OPTIONS` is always `405`.
- The Host allowlist and the Origin allowlist come from the bound port and are exactly
  `127.0.0.1:<port>` and `localhost:<port>`.
- Every request schema in `api/openapi.yaml` sets `additionalProperties: false`. A test fails
  otherwise, because the generated strict handler does not reject unknown fields itself.
- Tokens, cookies, CSRF values, codes and states never reach logs. The redacting handler drops
  attributes named `authorization`, `cookie`, `set-cookie`, `x-prd-csrf`, `token`, `launch_token`,
  `session`, `code`, `state`, `secret` and `password` (case-insensitive), and replaces any 43
  character base64url value under another key. The request log has method, path without the query,
  status and duration only. Bodies are never logged. The launch URL is printed to stderr with
  `--no-browser` and is never passed to `slog`.
- Launch tokens are compared with `subtle.ConstantTimeCompare` against every live token, without
  an early exit. Sessions and CSRF tokens are compared with `hmac.Equal`.
- The CSP is exactly the one in TECH_SPEC 8.5. Script hashes come from `csp.json` in the embedded
  web build, and a missing or malformed file leaves `script-src 'self'`. Entries that are not
  `'sha256-...'` sources are dropped. `csp.json` is never served.
- `Cache-Control: no-store` is set on `/api/*` and `/events`.
- Static pages must not use inline styles or inline event handlers. `style-src 'self'` blocks them,
  and `csp-hashes.mjs` fails the web build when it finds a `<style>` block or a `style` attribute.
- `go.mod` pins every dependency to an exact version. `oapi-codegen` is a `tool` directive.
- `SameSite=Strict` is why the OAuth callback cannot carry the cookie. It redirects to a static
  page that completes the flow with a protected `POST` ([auth.md](auth.md)). Do not add a session
  requirement to the callback, and do not add any other cookie-less route that takes input.
- `TestProtectedSurface` lists which paths are protected, so a new route has to be classified.
- The write endpoints `POST /api/prs/{id}/approve`, `/request-changes` and `/ready` pass the whole chain like the merge (session, CSRF, Origin, Sec-Fetch-Site, Host, rate limit, 64 KiB body, schema with `additionalProperties: false` and a hex `expectedHeadSha`), then re-check the pull request on GitHub (open, draft, head SHA, not opened by the viewer) before one write with the viewer's token through `github` and `netguard`. They are not proxies: each sends one fixed GitHub call built from validated parts ([review-actions.md](review-actions.md)).

## Tests

- [server/internal/api/api_test.go](../../server/internal/api/api_test.go): every check in the chain with a passing and a failing case (Host, OPTIONS, Sec-Fetch-Site, Origin, session, CSRF, content type, body size, invalid and unknown fields, rate limits and `Retry-After`), launch flow, cookie attributes, sliding re-issue, security headers on API, static and error responses, log hygiene, and the `additionalProperties` rule.
- [server/internal/api/static_test.go](../../server/internal/api/static_test.go): static serving.
- [server/internal/api/mw/mw_test.go](../../server/internal/api/mw/mw_test.go): chain order, panic recovery, access log, CSP and `csp.json` loading, rate limit rounding, the separate preview bucket, validator pass-through; [ratelimit_internal_test.go](../../server/internal/api/mw/ratelimit_internal_test.go): eviction of idle buckets.
- [server/internal/auth/launch_test.go](../../server/internal/auth/launch_test.go) and [session_test.go](../../server/internal/auth/session_test.go): single use, expiry, previous token, rotation, JSON launch file, forged cookies, key in the secrets store, legacy key import, revoke all.
- [server/internal/logging/redact_test.go](../../server/internal/logging/redact_test.go): redaction of every listed key, groups and `With`.
- [server/cmd/prdash/main_test.go](../../server/cmd/prdash/main_test.go): data dir modes, private file refusal, lock contention, bind address.
- [server/internal/api/mw/mw_test.go](../../server/internal/api/mw/mw_test.go) `TestProtectedSurface` and [oauth_api_test.go](../../server/internal/api/oauth_api_test.go): the cookie-less callback, start protection and the complete step.
- [e2e/github.spec.ts](../../e2e/github.spec.ts): the cross-site callback in a real browser.
- [server/internal/api/review_api_test.go](../../server/internal/api/review_api_test.go) `TestReviewEndpointsAreGuardedLikeTheMerge` (session, CSRF, Origin, Sec-Fetch-Site, Host, content type, size, one JSON value) and [e2e/review.spec.ts](../../e2e/review.spec.ts).
- [web/src/api/client.test.ts](../../web/src/api/client.test.ts) and [web/scripts/csp-hashes.test.ts](../../web/scripts/csp-hashes.test.ts).
