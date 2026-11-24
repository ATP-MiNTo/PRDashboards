# Security middleware

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 8.1 to 8.5. Related: [app-shell.md](app-shell.md).

## Purpose

Treats the browser as a remote client. Every request passes one middleware chain before it reaches
a handler, and every response carries the same security headers. Launch tokens and session cookies
are how the browser proves it was started by the person who ran the binary.

Differences from the spec in milestone 1:

- Session state is in memory. SQLite persistence and "Sign out all browsers" arrive with storage
  in milestone 2.
- The cookie signing key is the file `<data-dir>/session.key` (32 random bytes, mode 0600), created
  once. It moves to the keychain in milestone 2.
- The session cookie has no `Secure` flag. The app is served over plain `http` on loopback, and a
  `Secure` cookie is not sent back on `http://127.0.0.1`. `HttpOnly` and `SameSite=Strict` stay.
- `/events` is guarded like `/api/*` but has no route yet.

## Data model

- `prd_session` cookie: `base64url(sessionID) + "." + base64url(HMAC-SHA256(key, sessionID))`.
  Session id is 32 random bytes. `HttpOnly`, `SameSite=Strict`, `Path=/`, `Max-Age` 30 days.
- Session record (memory only): key is `sha256(sessionID)`, value is `created`, `lastIssued`,
  `expires`. The cookie is sent again when `lastIssued` is older than 24 h, and `expires` slides to
  30 days from the last issue.
- CSRF token: `base64url(HMAC-SHA256(key, "csrf:" + sessionID))`. Returned by `GET /api/session`,
  sent back as `X-PRD-CSRF` on every non-GET request.
- Launch token: 32 random bytes, base64url. Valid for 2 minutes, single use. The newest token is
  in `<data-dir>/launch.token` (mode 0600, written through a temporary file and a rename).
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

## Code map

Server:

- [server/internal/api/mw/problem.go](../../server/internal/api/mw/problem.go): problem writer, problem codes, `Protected`, `Chain`.
- [server/internal/api/mw/guards.go](../../server/internal/api/mw/guards.go): `HostAllowlist`, `NoCORS`, `SameOrigin` (Sec-Fetch-Site and Origin).
- [server/internal/api/mw/ratelimit.go](../../server/internal/api/mw/ratelimit.go): token buckets, 30 per minute per session and 10 per minute for session creation.
- [server/internal/api/mw/session.go](../../server/internal/api/mw/session.go): `Identify`, `RequireSession`, `RequireCSRF`.
- [server/internal/api/mw/body.go](../../server/internal/api/mw/body.go): content type, size, single JSON value, OpenAPI request validation (`kin-openapi`, with the spec embedded by the generator).
- [server/internal/api/mw/headers.go](../../server/internal/api/mw/headers.go): `CSP`, `LoadScriptHashes`, `SecurityHeaders`.
- [server/internal/api/mw/observe.go](../../server/internal/api/mw/observe.go): `AccessLog` and `Recover`.
- [server/internal/api/server.go](../../server/internal/api/server.go): `New` assembles the chain, and the `CreateSession` and `GetSession` handlers.
- [server/internal/auth/launch.go](../../server/internal/auth/launch.go): `Launcher` (issue, rotate, consume, remove).
- [server/internal/auth/session.go](../../server/internal/auth/session.go): `Sessions`, cookie and CSRF helpers, `LoadOrCreateKey`.
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
   session. `POST /api/session` uses its own bucket.
4. `Validate` reads the body (64 KiB cap), requires a single JSON value, then runs
   `openapi3filter.ValidateRequest`. Paths missing from the spec pass to the router, which answers
   `404 not_found`. A write without a body skips the content type and JSON checks, and the schema
   still rejects it when the body is required.
5. Launch: `Launcher.Consume` compares the presented token with the live tokens in constant time,
   spends it and issues a new one. `CreateSession` then calls `Sessions.Create` and sets the cookie.
6. Startup refuses to run when the data dir is group- or world-writable, and the lock is taken
   before the port is bound (see [app-shell.md](app-shell.md)).

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
- Follow-ups: keychain for the signing key and SQLite-backed sessions (milestone 2).

## Tests

- [server/internal/api/api_test.go](../../server/internal/api/api_test.go): every check in the chain with a passing and a failing case (Host, OPTIONS, Sec-Fetch-Site, Origin, session, CSRF, content type, body size, invalid and unknown fields, rate limits and `Retry-After`), launch flow, cookie attributes, sliding re-issue, security headers on API, static and error responses, log hygiene, and the `additionalProperties` rule.
- [server/internal/api/static_test.go](../../server/internal/api/static_test.go): static serving.
- [server/internal/api/mw/mw_test.go](../../server/internal/api/mw/mw_test.go): chain order, panic recovery, access log, CSP and `csp.json` loading, rate limit rounding, validator pass-through.
- [server/internal/auth/launch_test.go](../../server/internal/auth/launch_test.go) and [session_test.go](../../server/internal/auth/session_test.go): single use, expiry, previous token, rotation, forged cookies, key file.
- [server/internal/logging/redact_test.go](../../server/internal/logging/redact_test.go): redaction of every listed key, groups and `With`.
- [server/cmd/prdash/main_test.go](../../server/cmd/prdash/main_test.go): data dir modes, lock contention, bind address.
- [web/src/api/client.test.ts](../../web/src/api/client.test.ts) and [web/scripts/csp-hashes.test.ts](../../web/scripts/csp-hashes.test.ts).
