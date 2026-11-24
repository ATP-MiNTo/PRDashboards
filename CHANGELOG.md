# Changelog

All notable changes to PR Dashboards are listed here, newest first.

Versioning: [semver](https://semver.org/). The version lives in the `VERSION` file at the repo
root and stays `0.x` until v1 is complete. Every merged change bumps `VERSION` and adds a dated
entry for that version. There is no "Unreleased" section.

## [0.1.0] - 2026-11-24

### Added

- App skeleton: Go server on `127.0.0.1` with the Next.js static export embedded, single-instance
  lock, graceful shutdown and a placeholder page that shows the signed-in state.
- Security middleware: Host, Origin and `Sec-Fetch-Site` checks, no CORS, rate limits, session and
  CSRF checks, body limits with OpenAPI request validation, security headers with a script-hash
  CSP, panic recovery and a redacting log handler.
- Launch tokens and sessions: one-time launch URL, signed `prd_session` cookie with a sliding
  lifetime and a CSRF token (`POST /api/session`, `GET /api/session`).
- OpenAPI contract with generated Go and TypeScript types (`pnpm gen`).
- Start scripts for Linux, macOS and Windows that rebuild only when sources change.
- Storage and secrets: OS keychain with an encrypted-file fallback (AES-256-GCM, Argon2id,
  passphrase asked on the terminal), SQLite with WAL and numbered migrations for every table of
  the spec, a single-writer strategy, persisted sessions, `POST /api/sessions/revoke-all` and
  `GET/PUT /api/settings`. The session signing key moved into the secrets store.
- GitHub and Jira auth: setup of OAuth client credentials, GitHub OAuth with PKCE or a personal
  access token (scope checks, fine-grained warning, SSO block classifier), Atlassian 3LO with
  rotating refresh tokens under a mutex and a Reconnect state, and a Connected accounts panel. The
  OAuth callback is cookie-less and redirects to a static finish page that completes the flow.
- `netguard` outbound allowlist, `backoff` helper and a minimal GitHub client.
- Startup refuses `session.key`, `secrets.enc` and `data.db` that group or others can access. The
  launch file is JSON with the port, so a second start finds an instance on another port. The HTTP
  server has a 30 s `ReadTimeout`.
- End-to-end tests: fake GitHub and Atlassian servers (e2e build tag, `--fake`) and Playwright
  specs for the sign-in flows, run with `pnpm e2e`.
- CI jobs for Go, web, e2e, contract drift and docs, plus docs and version check scripts.

## [0.0.1] - 2026-11-24

### Added

- Product requirements, technical specification and architecture docs.
