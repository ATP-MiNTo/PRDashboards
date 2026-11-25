# Changelog

All notable changes to PR Dashboards are listed here, newest first.

Versioning: [semver](https://semver.org/). The version lives in the `VERSION` file at the repo
root and stayed `0.x` until v1 was complete. Every merged change bumps `VERSION` and adds a dated
entry for that version. There is no "Unreleased" section.

## [1.1.1] - 2026-11-25

### Fixed

- Merging with a fine-grained token that lacks a permission no longer says "An organization blocked
  access". The merge now says which GitHub permission the token needs (for example
  "contents: write"), and organization blocks are told apart as SAML SSO or an organization policy.
  A missing permission on a field the merge does not need, such as team review requests, no longer
  blocks the merge or adds a blocked-organization banner.
- GitHub refusals are logged with their status, message and the permissions GitHub accepts.

## [1.1.0] - 2026-11-25

### Changed

- GitHub sign-in uses a GitHub App instead of an OAuth App, so an organization owner can install it
  on selected repositories only, without approving the full `repo` scope. The dashboard shows the
  pull requests of the repositories the App is installed on. User tokens are refreshed
  automatically before they expire, and "Reconnect GitHub" appears if GitHub refuses a refresh.
  Existing OAuth App sign-ins and personal access tokens keep working. The README explains how to
  create and install the App.

### Added

- The GitHub setup card takes an optional App slug and shows an "Install on repositories" link.
- Connected accounts shows how GitHub is connected: GitHub App, OAuth or access token.

## [1.0.3] - 2026-11-25

### Changed

- The dashboard lists the open pull requests you opened as well as the ones assigned to you. GitHub
  never assigns the author of a pull request, so the list was empty for your own pull requests.
  A refresh runs two searches (assigned and opened by you) at the same time and shows a pull
  request found by both once. Merged pull requests are found the same way.

## [1.0.2] - 2026-11-25

### Fixed

- Jira sign-in failing on macOS with "data passed to Set was too big": the Atlassian token is too
  big for the keychain. Tokens and client secrets now live in an encrypted vault file
  (`~/.prdashboards/vault.enc`) whose key is the only item in the keychain. Items an earlier
  version saved in the keychain are moved into the vault on the first start.

## [1.0.1] - 2026-11-25

### Added

- README with requirements, how to run the app, first-time OAuth setup and a user manual.

### Fixed

- Feature docs for the app shell and auth no longer describe the installable app and the settings
  drawer as not built.

## [1.0.0] - 2026-11-24

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
- Poller and snapshot: a single-flight refresh on the GitHub and Jira intervals (doubled while the
  rate limit is low, with backoff), one paginated GraphQL search with per-check CI, a merged search
  only when PRs left the list, one transaction per refresh, a compressed stored snapshot, partial
  results for blocked organizations, `GET /api/snapshot` (ETag), `POST /api/sync` and the
  `GET /events` stream with `snapshot` and `sync-status` events.
- Command Table at `/`: top bar with theme switch (Light, Dark, Auto) and sync status, organization
  banner with an Authorize link, stat strip and filters, and the columns Repo/PR, Ticket, Last
  commit, Review, CI, Method and Merge (read only). The CI column lists every check with its link.
  The accounts panel moved to `/accounts/`.
- Stacked PRs are detected in one pass and drawn as grouped rows with "Merge after #N".
- Jira batching and ticket linking: keys from branch and title kept when the project exists,
  `key in (...)` in chunks of 100, project keys once per refresh, manual links and hidden keys
  (`PUT /api/prs/{id}/tickets`), a searchable picker of assigned tickets
  (`GET /api/tickets/assigned`), cached transitions (`GET /api/tickets/{key}/transitions`) and a
  per-row "Jira unreachable" state with a retry.
- Fake GitHub search and fake Jira for the e2e build, and a Playwright spec for an SSO-blocked
  organization.
- Merge method: a pure resolver (branch rules in order, repo default, fallback when the repo
  disallows a method), `mergeMethod` on every row, repo settings (`GET /api/repos`,
  `GET/PUT /api/repos/{id}`) and the Method column with a shared keyboard-accessible dropdown.
- Stacked PRs are enforced on the server: a child cannot be merged while its base is open.
- Merge and transition: `POST /api/prs/{id}/merge` with fresh checks, an idempotency key (24 hour
  table), merge, branch delete and up to four parallel Jira transitions in one recorded
  transaction, retries (`GET /api/retries`, `POST` and `DELETE /api/retries/{id}`) and the activity
  log (`GET /api/activity`). The merge panel lists every linked ticket with required fields and
  confirmation.
- Settings drawer with accounts, repositories and their branch rules, delete-branch default,
  polling intervals, theme, status sync rules, Multi-PR tickets, sessions and the activity log.
- Fake GitHub and Jira support for merge flows, and Playwright specs for merge with a transition,
  a failed move that is retried and a head that moved.
- Status sync rules: a pure engine with nine triggers, filters, modes All and Any and a
  fingerprint per firing, evaluated after every refresh and after a rules change. At most one
  transition per ticket, transitions fetched only for tickets that fire, a move recorded with its
  activity, firings and retries in one transaction, "needs input" for transitions with required
  fields. `GET/PUT /api/rules` and `POST /api/rules/preview`.
- Settings: an ordered rules editor (add, edit, enable, delete, drag or keyboard reorder, preview
  with "Don't move existing tickets"), the Multi-PR tickets section with its hint and an All/Any
  chip per rule, a "Sign out all browsers" action, and rule moves in the activity list.
- Ticket cells show "Waiting on 2 of 3 PRs: ..." and the merge panel preselects the transition of
  the matching `merged` rule or says "Won't move yet".
- Hardening: GitHub secondary rate limits without headers, source status in the top bar, last good
  data marked with its age on the banner and on every row, and the 1,000 result warning.
- Installable app: web app manifest, generated icons and a service worker that precaches the static
  app, shows an offline page that says to run `scripts/start.sh` and never touches `/api/*`.
- Playwright specs for a rate limit (`403` with a reset time and `429`) and for a ticket with
  three pull requests that moves after the third approval.

## [0.0.1] - 2026-11-24

### Added

- Product requirements, technical specification and architecture docs.
