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
- CI jobs for Go, web, contract drift and docs, plus docs and version check scripts.

## [0.0.1] - 2026-11-24

### Added

- Product requirements, technical specification and architecture docs.
