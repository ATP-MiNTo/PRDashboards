# PR Dashboard: Technical Specification

Status: v1, agreed 2026-11-24
Requirements: [PRD.md](PRD.md). Architecture and cross-cutting rules: [architecture.md](architecture.md).

This spec is the implementation contract for v1. Where it differs from the PRD, the PRD decisions
log has been updated to match. Feature docs in `docs/features/` are written with each feature and
become the source of truth for it once built.

## 1. Summary

A single Go binary that runs on the user's machine, binds to loopback only, and serves:

- a Next.js static export (the UI), embedded in the binary with `go:embed`
- a JSON API and a Server-Sent Events stream on the same origin

The backend owns every secret and every outside call. The browser holds no GitHub or Jira token and
can only ask the backend to perform a fixed set of operations, each validated and authorized on the
server. The browser and the backend are treated as if they were on different machines (section 8).

## 2. Stack

| Layer | Choice | Notes |
| --- | --- | --- |
| Backend language | Go 1.24+ | Single static binary, `net/http` with Go 1.22+ routing patterns |
| HTTP router | `net/http` `ServeMux` | No framework. Middleware chain written in `internal/api/mw` |
| API contract | OpenAPI 3.0.3 in `api/openapi.yaml` | 3.0 because `oapi-codegen` does not support 3.1. Go types and server interface via `oapi-codegen` (strict server, pinned as a `tool` in `go.mod`), TS types via `openapi-typescript`. Generated code is committed and `pnpm gen` regenerates it |
| GitHub client | Hand-written GraphQL over `net/http` | Queries live in `.graphql` files, embedded. REST only for merge and branch delete |
| Jira client | Hand-written REST v3 client | Jira Cloud via `api.atlassian.com/ex/jira/{cloudId}` |
| Storage | SQLite via `modernc.org/sqlite` | Pure Go, no cgo. WAL mode. Migrations embedded and versioned |
| Secrets | `github.com/zalando/go-keyring` | Fallback: AES-256-GCM file, key from Argon2id over a user passphrase |
| Frontend | Next.js 16, React 19, TypeScript, `output: 'export'` | App Router, client components only. No Next server at runtime |
| Frontend data | TanStack Query 5 | Mutations and cache. SSE pushes invalidations |
| Frontend styling | CSS Modules + CSS custom properties | Theme tokens from the prototype |
| Package manager | pnpm 10 (web), Go modules (server) | Lockfiles committed, versions pinned |
| Tests | Go `testing` + `-race`, Vitest + Testing Library, Playwright | See section 12 |

Node 22 LTS is required to build the UI only. It is not needed at runtime.

## 3. Repository layout

```text
api/openapi.yaml              API contract (source of truth)
api/oapi-codegen.yaml         oapi-codegen settings
go.work                       Workspace file, `use ./server`
server/                       Go module github.com/ATP-MiNTo/PRDashboards/server
  cmd/prdash/                 main: flags, wiring, browser launch
  internal/api/               handlers, static UI server, generated code (gen/), middleware (mw/)
  internal/auth/              OAuth flows, PAT entry, session cookies, launch tokens
  internal/secrets/           keychain + encrypted-file fallback
  internal/store/             SQLite access, migrations/, typed queries
  internal/github/            GraphQL queries, REST merge, rate-limit tracking
  internal/jira/              JQL search, transitions, fields metadata
  internal/linking/           ticket key parsing, PR-to-ticket resolution
  internal/stacks/            stacked PR detection
  internal/rules/             status sync rules engine (pure, no I/O)
  internal/merge/             merge method resolution, merge-and-transition orchestration
  internal/poller/            single-flight refresh scheduler, snapshot builder
  internal/events/            SSE hub
  internal/logging/           redacting slog handler
  internal/testutil/          fake clock and random source for tests
  internal/netguard/          outbound host allowlist transport
  internal/fake/              fake GitHub and Jira servers (build tag e2e only)
  web.go                      go:embed of web/ (package server)
  web/                        go:embed target, filled by the web build (only .gitkeep is committed)
web/                          Next.js app (static export)
  src/app/                    routes: / (dashboard), /setup, /auth/done
  src/components/             CommandTable, MergePanel, SettingsDrawer, ...
  src/api/                    generated types + typed fetch client
  src/theme/                  tokens and Auto theme logic
e2e/                          Playwright specs (boundary flows only)
scripts/                      start.sh, start.command, start.cmd, check scripts
docs/                         PRD, TECH_SPEC, architecture, features/
VERSION, CHANGELOG.md, CLAUDE.md
```

Boundaries:

- `rules`, `linking`, `stacks` and the method resolver in `merge` are pure: no network, no
  database, no clock (time is passed in). They hold the logic with the 90% coverage gate.
- Only `github`, `jira` and `auth` make outbound calls, and only through the `netguard` transport.
- Only `store` touches SQLite.
- The web app talks only to `/api/*` and `/events` on its own origin.

## 4. Running the app

### 4.1 Launch scripts

Modelled on RightRoom-3D's `dev-start` scripts:

- `scripts/start.sh` (Linux, Git Bash, macOS Terminal)
- `scripts/start.command` (double-click in macOS Finder, calls `start.sh`)
- `scripts/start.cmd` (Windows, double-click)

Each script:

1. Checks that Go and Node match `go.mod` and `.nvmrc`, and stops with a clear message if not.
2. Rebuilds only when sources changed since the last build (a hash of `web/`, `server/` and
   `api/` is stored in `.build-hash`): `pnpm -C web build`, copy `web/out` to `server/web`, then
   `go build -trimpath -o bin/prdash ./server/cmd/prdash`.
3. Runs `bin/prdash`. The binary opens the default browser on a one-time launch URL (4.3).

The scripts do not run `git pull` or switch branches. Updating is a separate, explicit step.

### 4.2 Binary

```text
prdash [--port 4317] [--data-dir ~/.prdashboards] [--no-browser]
```

- Listens on `127.0.0.1:<port>` only. The port is fixed (default 4317) because the Atlassian
  callback URL must match exactly. If the port is busy, the binary exits with a message. It never
  picks a random port and never binds `0.0.0.0` or `::`.
- Single instance: an exclusive lock on `<data-dir>/prdash.lock`. A second start finds the running
  instance, reads a fresh launch token from `<data-dir>/launch.token` (mode 0600) and opens the
  browser, then exits.
- Data dir is created with mode 0700. `data.db` and every file in it are 0600. The binary refuses
  to start if the data dir is group- or world-writable (POSIX).
- Start order: create the data dir, check its mode, take the lock, then bind the port. A busy port
  exits with a message.
- Until the keychain arrives (milestone 2), the cookie signing key is `<data-dir>/session.key`
  (32 random bytes, mode 0600). Sessions are in memory only until SQLite arrives.
- Logs go to stdout as JSON. With `--no-browser` the launch URL is printed to stderr and never
  logged. When the port is not 4317 the binary warns that OAuth callbacks need 4317.
- Graceful shutdown on SIGINT and SIGTERM: stop the poller, finish in-flight writes, close the DB.

### 4.3 Launch token and installable app

- On start, after each use and every 60 s, the server writes a new random 256-bit launch token to
  `launch.token` (atomic write, mode 0600). The current and the previous token are accepted while
  unexpired, each once. The file is removed on shutdown.
- The browser is opened at `http://127.0.0.1:4317/auth/launch#t=<token>`. The token is in the
  fragment, so it never reaches logs, history sync or `Referer`. A small static page posts it to
  `POST /api/session` and the server answers with the session cookie (8.3).
- From milestone 9, the UI ships a web app manifest and a service worker, so the browser's "Install app" adds a dock
  or taskbar icon. The installed app reuses the persistent session cookie. When the server is not
  running, the service worker shows an offline page that says to run the start script. The service
  worker caches static assets only, never `/api/*` responses.

## 5. Authentication with GitHub and Jira

### 5.1 First-run setup

`/setup` asks for the OAuth client IDs and secrets (GitHub OAuth App, Atlassian 3LO app). They are
stored in the keychain, never in the repo or the database. Setup also shows the exact callback URLs
to register:

- GitHub: `http://127.0.0.1:4317/oauth/github/callback`
- Atlassian: `http://127.0.0.1:4317/oauth/atlassian/callback`

### 5.2 GitHub

- Primary: OAuth App, authorization code flow with `state` and PKCE (S256). Scopes `repo read:org`.
- Fallback: personal access token pasted in Settings. Classic tokens need `repo read:org`.
  Fine-grained tokens are accepted with a warning that they only see the repos they were granted.
- The token is checked once with `viewer { login }` and the scopes header before it is stored.
- Blocked orgs: the GraphQL response's `errors` (type `FORBIDDEN`, SAML enforcement messages) and
  the `X-GitHub-SSO` header are mapped to an `orgBlocked` entry with the authorization URL. The UI
  shows the "access blocked, authorize org" banner from the prototype. The rest of the list still
  loads.

### 5.3 Jira (Atlassian OAuth 2.0 3LO)

- Scopes: `read:jira-work write:jira-work read:jira-user offline_access`.
- Authorization code flow with `state`. After the callback, `accessible-resources` lists the sites.
  v1 uses one site, picked in Settings when there are several.
- Access tokens last one hour. Refresh tokens rotate: every refresh stores the new refresh token
  before the access token is used. A single refresh runs at a time (mutex), so parallel calls never
  spend the same refresh token twice.
- A refresh failure marks Jira as disconnected, and the UI offers "Reconnect".

### 5.4 OAuth hardening

- `state` is 256 random bits, stored server-side with a 10-minute expiry, bound to the session, and
  deleted on first use.
- Callbacks accept only `GET` with `code` and `state`, and only when the request carries the
  session cookie that started the flow.
- After the callback the browser is redirected to `/auth/done`, which carries no token or code.

## 6. Storage

### 6.1 Secrets

Keychain service `prdashboards`. Items: `github.oauth.client`, `github.token`,
`atlassian.oauth.client`, `atlassian.token` (access + refresh + expiry as JSON),
`session.key` (HMAC key for cookies).

Fallback when no keychain is available: `secrets.enc`, AES-256-GCM, with a key derived with
Argon2id (64 MiB, 3 passes) from a passphrase asked on start. Without the passphrase, the app
starts locked.

Tokens are never written to SQLite, logs, crash output or API responses.

### 6.2 SQLite schema

WAL mode, `foreign_keys=ON`, `busy_timeout=5000`. One writer goroutine owns all writes. Every
multi-row change runs in one transaction. No per-row loops against the database.

| Table | Key | Purpose |
| --- | --- | --- |
| `schema_migrations` | `version` | Applied migrations |
| `settings` | `key` | Global settings as typed JSON values (polling intervals, theme, default method for new repos, delete branch after merge, Jira site) |
| `repos` | `id` (GitHub node id) | Repos the dashboard has seen: `name_with_owner`, `first_seen_at`, `default_method`, `is_new` (opens its settings on first sight) |
| `repo_branch_rules` | `repo_id, position` | Ordered `pattern` (exact or glob, `release/*`) to merge method |
| `prs` | `id` (node id) | Every PR ever shown: repo, number, title, head/base branch, head SHA, state (`open`, `merged`, `closed`), `merged_at`, last seen |
| `ticket_links` | `pr_id, ticket_key` | Link, `source` (`branch`, `title`, `manual`), `removed` flag for unlinked auto keys, position |
| `sync_rules` | `id` | Status sync rules (7.3): `position`, `name`, `enabled`, `trigger` JSON, `filters` JSON, `target_status`, `mode` (`all`/`any`, default `all`) |
| `rule_firings` | `rule_id, ticket_key` | Last fingerprint that fired for this rule and ticket (7.4) |
| `activity` | `id` | Log of every transition and merge: when, what, who triggered (rule id or manual), result, error |
| `pending_retries` | `id` | Failed transitions waiting for a retry: ticket, transition id, fields, last error |
| `snapshot` | `id = 1` | Last full snapshot as compressed JSON, so the UI shows data immediately on start |

Indexes: `prs(state, repo_id)`, `ticket_links(ticket_key)`, `activity(created_at DESC)`.

## 7. Domain logic

### 7.1 Ticket linking

Resolution for each PR (pure function in `linking`):

1. Keys from the head branch, then from the title, with regex `\b[A-Z][A-Z0-9]+-\d+\b`, kept in
   order and without duplicates.
2. Manual links in `ticket_links` are added. Keys the user removed are hidden.
3. Keys are kept only if the Jira project exists on the connected site (project keys are cached
   from one `GET /rest/api/3/project/search` per Jira refresh), so a branch like `UTF-8` is not
   linked by mistake.
4. No key: the ticket cell offers a picker that lists Jira tickets assigned to the user
   (`assignee = currentUser() AND statusCategory != Done`, one JQL, cached for the Jira interval).

A PR may have many tickets, and a ticket may have many PRs.

### 7.2 Stacked PRs

A PR is stacked on another when, in the same repo, its base branch equals the head branch of
another open PR. Detection is one pass over the snapshot with a map keyed by
`repo|headRefName`, so it is O(n) with no API calls. Stacks render as grouped rows, as in the
prototype. A child's Merge is disabled with "Merge after #<base>" until its base is merged.
A chained "merge whole stack" action is out of scope for v1.

### 7.3 Status sync rules

One engine keeps tickets in the right status during the whole PR lifecycle, including at merge.

A rule is:

```text
When <trigger> [and <filters>] then move ticket to <target status>   (mode: All | Any)
```

Triggers in v1:

| Trigger | Becomes true when | Fingerprint |
| --- | --- | --- |
| `review_submitted` | Any new review is submitted | latest review id |
| `review_approved` | Review decision is `APPROVED` | head SHA + decision |
| `changes_requested` | Review decision is `CHANGES_REQUESTED` | latest review id |
| `review_requested` | A review request exists | sorted requested reviewer ids |
| `ready_for_review` | PR is not a draft | head SHA at the time it became ready |
| `commit_stale` | Latest commit is older than N hours (N in the rule) | head SHA |
| `ci_failing` / `ci_passing` | CI rollup state changes to that value | head SHA + state |
| `merged` | PR is merged | merged PR id |

Filters (all optional, AND): repo (exact or glob), target branch (exact or glob), Jira project,
Jira issue type, current Jira status.

Evaluation, after every refresh, for each ticket that has at least one linked PR:

1. Collect the ticket's linked PRs (open ones from the snapshot, merged ones from `prs`).
2. Walk the enabled rules in order. A rule's condition holds when:
   - mode `any`: at least one linked PR meets the trigger and filters
   - mode `all`: every linked PR that passes the filters meets the trigger
3. The first rule whose condition holds and whose fingerprint differs from the last firing for
   that rule and ticket fires. At most one transition per ticket per refresh.
4. If the ticket is already in the target status, the firing is recorded and nothing is sent.
5. If the rule's data is unavailable (CI has not reported, Jira unreachable), the rule does not
   match.

Mode `all` uses the fingerprint of the whole set (sorted PR ids + each PR's fingerprint), so the
rule fires once, when the last PR meets the trigger.

### 7.4 Fire on change

- A rule fires when its condition becomes true with a new fingerprint. It does not re-apply on
  every refresh, so a manual move in Jira is not undone until something new happens on the PRs.
- Events that happened while the app was closed are caught up on the next refresh, because the
  fingerprint comparison is against the database, not against the previous in-memory state.
- The first time a rule is created or edited, it is evaluated against current data and the UI
  shows a preview of the tickets it would move. Saving the rule records current fingerprints only
  if the user picks "Don't move existing tickets".

### 7.5 Multi-PR tickets

- Default mode for every rule is `all`.
- The Settings drawer has a "Multi-PR tickets" section, with an ⓘ hint next to its title. It lists
  one chip per rule. Left-click sets **All**, right-click sets **Any** (the context menu is
  suppressed on the chip only). Enter sets All and Shift+Enter sets Any for keyboard use. The chip
  has `aria-pressed` and a label that says the current mode.
- The hint text: "When a ticket is linked to several PRs. All: the ticket moves only when every
  linked PR meets the rule. Any: the first PR that meets it moves the ticket. Left-click for All,
  right-click for Any."
- A ticket whose `all` rule is waiting shows "Waiting on 2 of 3 PRs: FE #34, proxy #56" in its
  ticket cell.
- Which PRs count: every PR the dashboard has linked to the ticket, including merged ones kept in
  `prs`. PRs that leave the open list are resolved as merged or closed with one extra search per
  refresh (`is:pr assignee:@me is:merged updated:>=<last refresh>`), never per PR. A closed
  (unmerged) PR stops counting.

### 7.6 Merge method

Resolution per PR (pure):

1. Repo branch rules, top to bottom, matched against the PR's base branch. First match wins.
2. Otherwise the repo default.
3. A repo seen for the first time gets the global "default for new repos" and opens its settings
   section, as in the prototype.
4. If the repo disallows the method (`squashMergeAllowed`, `mergeCommitAllowed`,
   `rebaseMergeAllowed` from the same GraphQL query), fall back to the first allowed method in the
   order merge commit, squash, rebase, and show the warning.
5. The per-PR picker in the Method column can override the result for that merge only.

This replaces the PRD's separate "production method" setting: a production branch is a branch rule
(`main` to merge commit, `production` to merge commit) seeded for every new repo.

### 7.7 Merge and transition

The Merge column button opens the merge panel. The panel lists every linked ticket with its
transition preselected from the first matching `merged` rule (or "Don't move" when none matches),
plus the merge method and the delete-branch option. Every ticket must be confirmed in the panel.
Required Jira fields for a chosen transition render inline. Tickets waiting on other PRs (mode
`all`) show "Won't move yet: waiting on ..." and can be forced with the dropdown.

Disabled states (checked by the UI for display and again by the server before acting): draft,
merge conflicts, required checks failing or pending, required reviews missing, base not merged
(stack), Jira unreachable when a transition is selected. Branch protection is never bypassed.

Server flow for `POST /api/prs/{id}/merge`:

1. Validate the body and the `Idempotency-Key` header. A repeated key returns the first result.
2. Re-fetch that PR by node id (one GraphQL call) and check: open, `headRefOid` equals the
   `expectedHeadSha` sent by the UI, `mergeable == MERGEABLE`, `mergeStateStatus` is `CLEAN` or
   `HAS_HOOKS`, and stack base merged. Any mismatch returns `409` with the fresh state.
3. Check that each requested transition id is in the ticket's current transitions list (fetched or
   cached at most 60 s) and that required fields are present.
4. `PUT /repos/{o}/{r}/pulls/{n}/merge` with `merge_method` and `sha`. GitHub rejects the merge if
   the head moved.
5. Optionally delete the head branch, only if it is in the same repo and no other open PR uses it
   as a base.
6. Transition each ticket, at most 4 in parallel. Each result is written to `activity`. Failures go
   to `pending_retries` and the UI shows Retry for that ticket. The merge is never rolled back.
7. Record the `merged` rule fingerprints for the tickets handled here, so the sync engine does not
   move them again.
8. Return per-step results and publish a `snapshot` event.

## 8. Security

### 8.1 Principle

The browser and the backend are treated as if they were on different devices. Being on loopback is
not a reason to trust a request. Every request is authenticated, every input is validated, and
every write is re-checked on the server against fresh data. The frontend only displays and asks.

### 8.2 Threat model

| Threat | Example | Control |
| --- | --- | --- |
| Other devices on the network | Laptop on café Wi-Fi | Bind to `127.0.0.1` only |
| Malicious website in the same browser | A page posts to `127.0.0.1:4317/api/...` (CSRF), or scans local ports | Session cookie with `SameSite=Strict`, Origin and `Sec-Fetch-Site` checks, CSRF header, no CORS |
| DNS rebinding | `evil.example` resolves to 127.0.0.1 | `Host` header allowlist: `127.0.0.1:4317`, `localhost:4317` only |
| Other local processes or users | A process calls the API with curl | No session cookie, so rejected. The launch token file is 0600 |
| XSS through PR titles, branch names, ticket summaries | A title containing `<script>` | React escaping, no `dangerouslySetInnerHTML`, strict CSP, URL allowlist for links |
| Token theft | Logs, API responses, browser storage | Tokens only in the keychain, never sent to the browser, redacted in logs |
| SSRF or redirect abuse | A Jira field pointing to another host | Outbound host allowlist on the HTTP transport |
| Replay or double submit | Double-click Merge | `Idempotency-Key`, expected head SHA |
| Stale UI state | PR changed after the page loaded | Server re-fetches and re-checks before every write |
| Supply chain | Compromised dependency | Pinned versions, lockfiles, `govulncheck`, `pnpm audit`, Dependabot |

Out of scope: malware already running as the same OS user with keychain access.

### 8.3 Sessions

- `POST /api/session` exchanges a valid one-time launch token for a session. Launch tokens are
  single use, expire after 2 minutes, and compared in constant time.
- Cookie `prd_session`: random 256-bit id, HMAC-signed with `session.key`, `HttpOnly`,
  `SameSite=Strict`, `Path=/`, no `Secure` flag. The app is plain `http` on loopback, and a
  `Secure` cookie would not be sent back on `http://127.0.0.1`, so the flag would break sign-in.
  Lifetime 30 days, sliding (the cookie is sent again when the last issue is older than 24 h), so
  the installed app keeps working. Sessions are stored hashed (SHA-256 of the id) in memory, and
  in SQLite from milestone 2, and can be revoked from Settings ("Sign out all browsers").
- A CSRF token bound to the session is returned by `GET /api/session` and must be sent as
  `X-PRD-CSRF` on every non-GET request.

### 8.4 Request checks (middleware order)

1. `Host` must be in the allowlist, otherwise `421`.
2. Reject any request with a CORS preflight (`OPTIONS`) or that would need CORS. No
   `Access-Control-*` header is ever sent.
3. `/api/*` and `/events`: `Sec-Fetch-Site` must be `same-origin` (or absent for non-browser
   clients, which then fail step 5). Non-GET requests must also have `Origin` equal to
   `http://127.0.0.1:4317` or `http://localhost:4317`.
4. Write endpoints are rate limited (30 per minute) so a compromised page cannot spam merges or
   transitions. The bucket is per session, with one shared bucket for requests without a valid
   session. `POST /api/session` has its own bucket of 10 per minute. Limited requests get `429`
   with `Retry-After`. The session cookie is read before this step, only to pick the bucket.
5. Valid session cookie, otherwise `401`. `POST /api/session` is the only exception.
6. Non-GET: valid `X-PRD-CSRF`, otherwise `403`. `POST /api/session` is the only exception.
7. Body of non-GET requests: `Content-Type: application/json`, max 64 KiB, one JSON value, then
   validation against the OpenAPI schema. Every request schema sets `additionalProperties: false`,
   so unknown fields are rejected (a test enforces this).

Each rejection is an `application/problem+json` response with a stable `code`. The implementation
and the code table are in [features/security.md](features/security.md).

### 8.5 Response headers

```text
Content-Security-Policy: default-src 'none'; script-src 'self' <sha256 hashes of Next inline scripts>;
  style-src 'self'; img-src 'self' data: https://avatars.githubusercontent.com;
  font-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self';
  frame-ancestors 'none'; base-uri 'none'; form-action 'self'
X-Content-Type-Options: nosniff
Referrer-Policy: no-referrer
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
Permissions-Policy: camera=(), microphone=(), geolocation=()
Cache-Control: no-store            (on /api/* and /events)
```

The web build computes the hashes of every inline script in the export and writes them to
`server/web/csp.json`, which the server reads at start. Fonts are bundled, not loaded from Google
Fonts.

### 8.6 Outbound calls

`netguard` wraps the HTTP transport and allows only: `api.github.com`, `github.com` (OAuth),
`api.atlassian.com`, `auth.atlassian.com`. Redirects to other hosts fail. TLS 1.2 minimum.
Timeouts: 10 s connect, 30 s total.

### 8.7 Output safety

- Links in the UI are built by the frontend from validated parts (`https://github.com/<owner>/<repo>/pull/<n>`,
  `https://<site>.atlassian.net/browse/<key>`). URLs from API data are never put in `href` as is.
- Logs are structured (`log/slog`). A redacting handler removes `Authorization` headers, tokens,
  codes and cookies.

### 8.8 Writes need intent

Every write comes from an explicit user action or a rule the user created. There is no API that
lets the frontend run an arbitrary GitHub or Jira call. The API exposes named operations only.

## 9. API

Source of truth: `api/openapi.yaml`. Main operations:

| Method and path | Purpose |
| --- | --- |
| `POST /api/session` | Exchange launch token for a session |
| `GET /api/session` | Session info and CSRF token |
| `GET /api/snapshot` | Full dashboard snapshot (ETag) |
| `GET /events` | SSE: `snapshot` (new version id), `activity`, `sync-status` |
| `POST /api/sync` | Trigger a refresh now (joins the running one if any) |
| `GET /api/tickets/{key}/transitions` | Available transitions and required fields |
| `GET /api/tickets/assigned` | Picker source |
| `PUT /api/prs/{id}/tickets` | Set manual links and removed keys |
| `POST /api/prs/{id}/merge` | Merge and transition (7.7) |
| `POST /api/retries/{id}` | Retry a failed transition |
| `GET/PUT /api/settings` | Global settings |
| `GET/PUT /api/repos/{id}` | Repo default and branch rules |
| `GET/PUT /api/rules` | Ordered sync rules (whole list, one transaction) |
| `POST /api/rules/preview` | Tickets a draft rule would move now |
| `GET /api/activity` | Activity log, paginated |
| `GET /oauth/{provider}/start`, `GET /oauth/{provider}/callback` | OAuth flows |
| `PUT /api/github/pat` | Store a PAT |

Errors use `application/problem+json` with a stable `code` (`org_blocked`, `head_moved`,
`not_mergeable`, `transition_unavailable`, `rate_limited`, ...).

## 10. Data fetching and rate limits

### 10.1 Refresh

- GitHub every 60 s, Jira every 5 min, both configurable (1, 2, 5, 15, 60 min), plus the Sync
  button. Polling runs only while the binary runs.
- Single flight: one refresh at a time. A Sync click during a refresh joins it. Browser tabs never
  trigger outside calls by themselves; they read the snapshot and listen to SSE.
- A refresh produces a new immutable snapshot. The UI fetches it only when the SSE version changes,
  using `ETag`.

### 10.2 GitHub calls per refresh

1. One paginated GraphQL search, 50 per page:
   `search(query: "is:pr is:open assignee:@me archived:false", type: ISSUE, first: 50, after: $c)`.
   Per PR it selects: id, number, title, url, isDraft, mergeable, mergeStateStatus, reviewDecision,
   headRefName, headRefOid, baseRefName, repository { id nameWithOwner squashMergeAllowed
   mergeCommitAllowed rebaseMergeAllowed }, `commits(last: 1) { commit { committedDate
   statusCheckRollup { state } } }`, `latestReviews(first: 10) { id state submittedAt author { login } }`,
   `reviewRequests(first: 10) { requestedReviewer { ... on User { id } ... on Team { id } } }`.
   The query also selects `rateLimit { cost remaining resetAt }`.
2. One search for recently merged PRs (7.5), only when PRs left the open list.

No per-PR follow-up calls. The only single-PR call is the re-check right before a merge.

### 10.3 Jira calls per refresh

1. `POST /rest/api/3/search/jql` with `key in (...)`, chunks of 100 keys, fields
   `summary,status,project,issuetype`. A key that no longer exists is reported as missing, not as
   an error for the whole list.
2. Project key list (7.1), once per Jira refresh.
3. Transitions are fetched only when needed (merge panel, rule firing, retry) with
   `expand=transitions.fields`, and cached per ticket for 60 s.

### 10.4 Limits and backoff

- GitHub: when `remaining` falls under 10% of the limit, the interval doubles until `resetAt`.
  On `403` or `429` with `Retry-After` or `x-ratelimit-reset`, wait that long. Secondary limits use
  exponential backoff with jitter (base 2 s, max 5 min).
- Jira: on `429` or `503`, honour `Retry-After`, otherwise exponential backoff with jitter.
- At most 4 parallel outbound requests per provider.
- The search cap of 1,000 results shows a warning when reached.

### 10.5 Partial failures

The snapshot carries per-source status: `github` (ok, rate limited, error), each blocked org, and
`jira` (ok, unreachable, disconnected). Rows affected by a failure show an error state (for
example "Jira unreachable, retry the lookup") while the rest of the table stays usable. The last
good data is kept and marked with its age.

## 11. Frontend

### 11.1 Layout

Based on the Command Table prototype:

- Top bar: brand, theme switch (Light, Dark, Auto), last sync times, next sync countdown, Sync
  button, settings button. Org-blocked banner below it.
- Stat strip (open, ready, blocked, draft, stale) and filter chips.
- Table columns, in this order: **Repo/PR, Ticket, Last commit, Review, CI, Method, Merge**.
  Stacks are grouped rows with a connector line. Each ticket shows key, status, title, matched rule
  and source (branch, title, manual), plus unlink and the picker.
- Merge panel (7.7) opens inline under the row.
- Settings drawer on the right with sections: Connected accounts, Repositories (search, default
  for new repos, per-repo default and branch rules, delete branch after merge), Status sync rules
  (ordered list, add, edit, drag to reorder, enable, preview), Multi-PR tickets (7.5), Polling,
  Activity, Sessions.

### 11.2 Behaviour

- Dropdowns open on hover and on click, with full keyboard support (arrow keys, Home, End, Enter,
  Escape), as in the prototype.
- Theme: Auto picks light from 07:00 to 19:00 local time and dark otherwise. The choice is stored
  in settings on the server. Tokens are CSS custom properties, using the prototype's pastel palette.
- Per-row components subscribe to their own PR in the snapshot, so a change re-renders only that
  row.
- Times are shown relative ("22m ago") with the absolute time in a tooltip.
- `prefers-reduced-motion` turns off animations.

### 11.3 Not in v1

Mobile layout, chained stack merge, multiple Jira sites, notifications.

## 12. Testing

| Layer | Tool | Scope |
| --- | --- | --- |
| Pure logic | Go table tests | `rules`, `linking`, `stacks`, merge method resolution. Coverage gate 90% lines |
| Provider clients | Go + `httptest` with recorded fixtures | GraphQL parsing, pagination, rate-limit headers, SSO errors, Jira chunking, token refresh rotation |
| HTTP layer | Go + `httptest` | Every middleware check in 8.4 has a passing and a failing test (Host, Origin, Sec-Fetch-Site, session, CSRF, body size, unknown fields, rate limit) |
| Store | Go with a temp SQLite file | Migrations up from empty, transactions, constraints |
| UI | Vitest + Testing Library | Command Table rendering, merge panel, settings drawer, Multi-PR chips (left, right, keyboard), theme logic |
| End to end | Playwright | Boundary flows only (below) |

All Go tests run with `-race`. No test calls the real GitHub or Jira.

Playwright stays lean and covers only the parts that cross into systems we don't control. It runs
against the real binary built with `-tags e2e --fake`, which starts in-process fake GitHub and Jira
servers that serve fixtures and can be told to fail:

1. GitHub OAuth sign-in and callback (state check, bad state rejected), and PAT entry.
2. Atlassian sign-in, site pick, token refresh, and refresh failure leading to Reconnect.
3. Org blocked by SSO shows the banner, and the rest of the list loads.
4. Rate limit (`403` with reset, `429`) shows the waiting state and recovers.
5. Merge and transition, including a required field, and a transition failure followed by Retry.
6. Head moved between page load and Merge returns the conflict and refreshes the row.

The `e2e` build tag is the only way to include the fakes, so the release binary cannot serve fake
data.

## 13. CI

GitHub Actions on every pull request:

1. `go vet`, `golangci-lint`, `go test -race -coverprofile` with the coverage gate, `govulncheck`
2. `pnpm install --frozen-lockfile`, typecheck, ESLint, Prettier check, Vitest, `pnpm build`
3. OpenAPI drift check: regenerate Go and TS code and fail if the diff is not empty
4. Docs check: required headings, links, feature index
5. Version check: `VERSION` changed and `CHANGELOG.md` has a dated entry for it
6. Playwright e2e against the `e2e` build, from milestone 3 (there is no outside boundary before
   OAuth)

## 14. Versioning and changelog

- `VERSION` at the repo root, semver, `0.x` until v1 is complete.
- Every merged change bumps `VERSION` and adds a dated entry for that version at the top of
  `CHANGELOG.md`. There is no "Unreleased" section.
- Prebuilt binaries (goreleaser) are a later item. v1 builds from source with the start scripts.

## 15. Conventions

- Conventional Commits: `feat:`, `fix:`, `docs:`, `test:`, `chore:`, `refactor:`.
- Branch names: `type/TICKET-title-in-kebab-case`, for example `feat/PRD-3-oauth-flows`. The
  ticket prefix is `PRD-<n>` until a Jira project exists for this repo.
- PR body: ticket line, then `# Overview`, `# Change`, and a `Note:` line.

## 16. Milestones

1. Skeleton: Go server, embed, start scripts, security middleware, session and launch token, CI.
2. Storage and secrets: SQLite, migrations, keychain and encrypted fallback.
3. OAuth flows and PAT fallback, org blocked handling.
4. Poller and GitHub snapshot, Command Table read only.
5. Jira batching and ticket linking, picker, manual links.
6. Merge method resolution, repo settings, stacks.
7. Merge panel with transitions, retries, activity log.
8. Status sync rules engine, preview, Multi-PR tickets section.
9. Hardening: rate limits, partial failures, e2e boundary flows, PWA install.

## 17. Later

- Chained "merge whole stack".
- Prebuilt binaries via goreleaser.
- Multiple Jira sites.
- Jira development panel as an extra PR source for multi-PR tickets.
