# Poller and snapshot

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 9, 10 and 11.2. Related: [status-sync-rules.md](status-sync-rules.md), [command-table.md](command-table.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [storage.md](storage.md), [auth.md](auth.md).

## Purpose

Refreshes the list of open pull requests assigned to the user or opened by the user from GitHub and the tickets from Jira,
builds one immutable snapshot, stores it and tells the browser. One refresh runs at a time, and a
Sync click during a refresh joins it. Browser tabs never cause outside calls.

Differences from the spec:

- After the fetch steps a refresh evaluates the status sync rules from the data it has and moves the tickets that fire ([status-sync-rules.md](status-sync-rules.md)). The result is part of the refresh transaction. A rules change evaluates again from cached data (`Reevaluate`, same as `Relink`).
- The stream carries `snapshot`, `sync-status` and `activity` (rule moves and merges).

## Data model

- API schemas: `Snapshot`, `SnapshotPr`, `SnapshotRepo`, `Ci`, `Check`, `Review`, `StackInfo`, `SnapshotTicket` (with `waiting` and `mergePlan`), `Sources`, `SourceStatus`, `BlockedOrg`, `Warning`, `SyncStatus` in [api/openapi.yaml](../../api/openapi.yaml). `version` is an integer that grows with every build of the snapshot and continues after a restart.
- `SnapshotPr.roles` says why a PR is listed: `assigned` when the assigned search returned it, `authored` when the authored search did, both when both did (never empty). `SearchOpen` sets `github.PR.Roles` while it merges the two result sets by PR id, and `build` maps it (`rolesOf`). Roles live in the snapshot JSON only, not in a SQLite column, so there is no migration. A snapshot stored before roles existed has none: `Load` gives such a PR both roles (`fillRoles`) and re-encodes the JSON it serves, so the UI still shows it. The next refresh replaces them with the real ones.
- `snapshot` table: one row (`id = 1`) with `version`, gzip-compressed JSON `data` and `updated_at`.
- `repos` (`is_new` until the user has seen the repo), `repo_branch_rules` (seeded `main` and `production` to `merge`), `prs` (`state` open, merged or closed, `last_seen_at`) and `ticket_links` are written by one transaction per refresh ([storage.md](storage.md)). New repos get the global default method. The refresh reads the stored repo configuration (`Store.Repos`) so `build` can resolve `SnapshotPr.mergeMethod` ([merge-method.md](merge-method.md)). `Linked` and `Refs` give the merge flow the ticket keys and the head and base branches of the open PRs from the current snapshot.
- Settings read: `polling.githubSec`, `polling.jiraSec` (re-read before every wait, so a change applies without a restart).

## Code map

- [server/internal/poller/poller.go](../../server/internal/poller/poller.go): `Poller`, `Config`, interfaces `Store`, `GitHub`, `Jira`, `Accounts`, `Publisher`, `Run`, `plan`, `Interval`, `Sync`, `Trigger`, `Relink`, `Reschedule`, `Status`, `Load`.
- [server/internal/poller/refresh.go](../../server/internal/poller/refresh.go): `refresh`, GitHub and Jira fetch steps, failure handling, merged-search resolution, snapshot persistence.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): `build`, the pure assembly of a `gen.Snapshot` from fetched data, links and stacks.
- [server/internal/poller/sync.go](../../server/internal/poller/sync.go): rule evaluation and the moves. [server/internal/store/syncrules.go](../../server/internal/store/syncrules.go): `SaveSync`, `SyncState`.
- [server/internal/github/search.go](../../server/internal/github/search.go), [open_prs.graphql](../../server/internal/github/open_prs.graphql), [merged_prs.graphql](../../server/internal/github/merged_prs.graphql): `SearchOpen`, `SearchMerged`, check mapping, rate limit and block classification. Only SAML SSO and organization policy refusals become blocked organizations; a FORBIDDEN about a missing token permission goes to `SearchResult.Errors` and the rest of the data still loads.
- [server/internal/store/refresh.go](../../server/internal/store/refresh.go): `SaveRefresh`, `SaveSnapshot`, `LoadSnapshot`, `OpenPRs`, `UserLinks`, `SetUserLinks`, `SeedRules`. [repos.go](../../server/internal/store/repos.go): `Repos`.
- [server/internal/events/events.go](../../server/internal/events/events.go): the SSE `Hub`.
- [server/internal/api/handlers_snapshot.go](../../server/internal/api/handlers_snapshot.go): `GetSnapshot`, `SyncNow`.
- [server/cmd/prdash/run.go](../../server/cmd/prdash/run.go) and [accounts.go](../../server/cmd/prdash/accounts.go): wiring and the `Accounts` adapter.

## Key flows

1. Start: `Load` restores the stored snapshot (a corrupt one is ignored), then `Run` refreshes at once.
2. `Run` reads the settings, `plan` computes when GitHub and Jira are due (interval, doubled while the rate limit is low, or the backoff, whichever is later), runs a refresh when due and otherwise waits for the timer or `Reschedule`.
3. `refresh`: GitHub searches (`SearchOpen`: assigned and authored, in parallel, merged by PR id), merged searches when PRs left, user links, Jira site, project keys, ticket lookup, repo states, `runRules`, `build`, encode and compress, `SaveRefresh` (one transaction), publish `snapshot` then `sync-status`.
4. A fetch failure never fails the refresh. GitHub rate limits (`403` or `429` with `Retry-After` or `x-ratelimit-reset`, and a `403` whose body names a secondary rate limit, which backs off exponentially from 2 s up to 5 min) and errors keep the last list and record `rate_limited` or `error` with `resetAt`. The web shows the state in the top bar, a banner with the age of the data and a "Last data" mark on every row ([command-table.md](command-table.md)). A rejected token, a GitHub App connection that needs a reconnect (error text "Reconnect GitHub") or a missing connection clears the list and records `disconnected`. Jira failures mark rows `ticketLookup: unreachable` and keep the last tickets.
5. Sync button: `POST /api/sync` calls `Trigger` (202, joins the running refresh). Connecting or disconnecting an account and changing the Jira site also call `Trigger`. Saving settings calls `Reschedule`.
6. Manual link changes: `PUT /api/prs/{id}/tickets` stores them and `Relink` rebuilds the snapshot from memory, asking Jira only for keys it has not seen ([ticket-linking.md](ticket-linking.md)).
7. Browser: `GET /api/snapshot` with `If-None-Match` answers `304` when the version is unchanged. `GET /events` streams `snapshot {version}` and `sync-status {running, githubAt, jiraAt, nextGithubAt, nextJiraAt}`.

## Outside calls

Per refresh, independent of the number of PRs:

- GitHub: two GraphQL searches, `assignee:@me` and `author:@me`, run concurrently (two requests in flight) and merged by PR id in one in-memory pass (the roles come from which search returned the PR), so a PR that is both assigned and authored is one row with both roles and no extra GitHub call. Each search is one request per 50 PRs (120 PRs found by both are 6 requests). Plus two merged searches (`assignee:@me` and `author:@me`, also concurrent) only when PRs left the open list. The same query returns the per-check CI. `Capped` is set when either search is capped, and `Total` is the larger of the unique PRs returned and the biggest count GitHub reported. If either search fails on its first page the whole GitHub fetch fails with that error (a rejected token first, then a rate limit, then any other). Rate limit: costs add up, the remaining budget is the lowest one seen.
- Jira (on the Jira interval): one project search page per 50 projects, one JQL per 100 ticket keys (120 keys are 2 requests). Between Jira intervals only keys not looked up before are asked.
- Rule firings add one transitions lookup (cached 60 s) and one move per ticket that fires, nothing else.
- Backoff: GitHub rate limits wait for `Retry-After` or `resetAt`, other errors use `internal/backoff`. Jira honours `Retry-After` on `429` and `503`.

## Rules and invariants

- Lifecycle: `New` creates the base context of every refresh, so `Sync`, `Trigger` and `Relink` work before `Run`. When `Run`'s context ends it cancels that context, refuses new refreshes (a flight that is already done with `context.Canceled`; a later `Run` starts afresh) and returns only after the running refresh goroutine has finished (a `sync.WaitGroup`), so nothing touches the database after the server closes it.

- One refresh at a time. `Sync` and `Trigger` join the running one. `Relink` waits for it.
- Exactly one store transaction per refresh that fetched GitHub data, including the rule activity, firings and retries. A refresh without a GitHub fetch writes the snapshot row, and the rule result when there is one.
- The snapshot is immutable once published. `Current` returns an atomic pointer, and `Published.JSON` is encoded once and served as is.
- Fine-grained tokens cannot read check runs through GraphQL: GitHub answers `FORBIDDEN` ("Resource not accessible by personal access token", `saml_failure` and `ip_allow_list_failure` false) once per check, with a path through `statusCheckRollup` and `contexts`, and returns null context nodes, but still sends the rollup state and `totalCount`. This is a known limitation, not an error: the github package drops these errors (`hiddenCheckError`), so `SearchResult.Errors` stays empty, no `github_partial` warning and no organization banner are shown, and merged-PR resolution is unaffected. The pull request is kept and the CI column shows only the overall state and "N checks" linking to GitHub's checks page. The log gets one DEBUG line per page ("github hid check runs from this token", with a count) instead of one WARN per check. A `FORBIDDEN` error elsewhere, or with an SSO or policy flag, is still reported or blocked as before. The `saml_failure` and `ip_allow_list_failure` flags of an error decide SSO or policy blocks before any wording is guessed.
- PRs that left the list are resolved only from a complete search: not capped, not partial, no GraphQL errors (other than the hidden check runs above) and no blocked organization. Otherwise they stay open.
- Tokens never enter the snapshot, the log or an event. Errors in the snapshot are fixed, user-facing texts.
- The SSE hub keeps the last event per name, replays it to a new client, drops a client whose buffer is full and sets a write deadline per write. `WriteTimeout` of the HTTP server stays unset.
- `go test -race` covers the single flight with concurrent `Sync` calls (exactly one outbound refresh).

## Tests

- [server/internal/poller/poller_test.go](../../server/internal/poller/poller_test.go) and [n1_test.go](../../server/internal/poller/n1_test.go): snapshot content and schema conformance, single flight, persistence and reload, one transaction per refresh, merged search rules, failures and recovery, interval adaptation, `Run` scheduling, relink, and the no-N+1 check with 120 PRs (two open searches of 3 pages each, no merged search, 2 Jira searches, 1 project call).
- [server/internal/poller/sync_test.go](../../server/internal/poller/sync_test.go): rule moves and call counts. The secondary limit and Jira `429` cases are in poller_test.go, and the `403` fixtures with a reset time or a secondary limit body in [search_test.go](../../server/internal/github/search_test.go). Hidden check runs (no errors, SSO still blocks, other `FORBIDDEN` errors kept) are in [permission_test.go](../../server/internal/github/permission_test.go) and the `hiddenCheckError` table in [hidden_check_test.go](../../server/internal/github/hidden_check_test.go).
- [e2e/ratelimit.spec.ts](../../e2e/ratelimit.spec.ts): a `403` with a reset time and a `429` show the waiting state and recover on the next sync.
- [server/internal/events/events_test.go](../../server/internal/events/events_test.go): replay, heartbeat, slow client dropped, close.
- [server/internal/store/refresh_test.go](../../server/internal/store/refresh_test.go), [server/internal/github/search_test.go](../../server/internal/github/search_test.go): both queries sent, union deduped by id, cap, incomplete, blocked, errors and rate limit merging, one search failing, the merged search union.
- [server/internal/poller/roles_test.go](../../server/internal/poller/roles_test.go): roles in the snapshot, the both-roles fallback, an older stored snapshot without roles loading, roles surviving a restart. `TestSearchOpenSendsBothQueriesAndMergesByID` in search_test.go checks the roles of PRs found by one or both searches.
- [server/internal/api/snapshot_api_test.go](../../server/internal/api/snapshot_api_test.go): ETag and 304, sync, SSE needs a session and heartbeats.
- [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts): includes a PR the user opened that is assigned to nobody (the fake GitHub honours `assignee:@me` and `author:@me`, [github_search.go](../../server/internal/fake/github_search.go)).
