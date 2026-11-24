# Poller and snapshot

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 9, 10 and 11.2. Related: [command-table.md](command-table.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [storage.md](storage.md), [auth.md](auth.md).

## Purpose

Refreshes the list of open pull requests assigned to the user from GitHub and the tickets from Jira,
builds one immutable snapshot, stores it and tells the browser. One refresh runs at a time, and a
Sync click during a refresh joins it. Browser tabs never cause outside calls.

Differences from the spec:

- A refresh does not evaluate rules or move tickets yet. Those arrive with milestone 8.
- The snapshot has no `activity` event. The stream carries `snapshot` and `sync-status`.

## Data model

- API schemas: `Snapshot`, `SnapshotPr`, `SnapshotRepo`, `Ci`, `Check`, `Review`, `StackInfo`, `SnapshotTicket`, `Sources`, `SourceStatus`, `BlockedOrg`, `Warning`, `SyncStatus` in [api/openapi.yaml](../../api/openapi.yaml). `version` is an integer that grows with every build of the snapshot and continues after a restart.
- `snapshot` table: one row (`id = 1`) with `version`, gzip-compressed JSON `data` and `updated_at`.
- `repos` (`is_new` until the user has seen the repo), `repo_branch_rules` (seeded `main` and `production` to `merge`), `prs` (`state` open, merged or closed, `last_seen_at`) and `ticket_links` are written by one transaction per refresh ([storage.md](storage.md)). New repos get the global default method. The refresh reads the stored repo configuration (`Store.Repos`) so `build` can resolve `SnapshotPr.mergeMethod` ([merge-method.md](merge-method.md)). `Linked` and `Refs` give the merge flow the ticket keys and the head and base branches of the open PRs from the current snapshot.
- Settings read: `polling.githubSec`, `polling.jiraSec` (re-read before every wait, so a change applies without a restart).

## Code map

- [server/internal/poller/poller.go](../../server/internal/poller/poller.go): `Poller`, `Config`, interfaces `Store`, `GitHub`, `Jira`, `Accounts`, `Publisher`, `Run`, `plan`, `Interval`, `Sync`, `Trigger`, `Relink`, `Reschedule`, `Status`, `Load`.
- [server/internal/poller/refresh.go](../../server/internal/poller/refresh.go): `refresh`, GitHub and Jira fetch steps, failure handling, merged-search resolution, snapshot persistence.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): `build`, the pure assembly of a `gen.Snapshot` from fetched data, links and stacks.
- [server/internal/github/search.go](../../server/internal/github/search.go), [open_prs.graphql](../../server/internal/github/open_prs.graphql), [merged_prs.graphql](../../server/internal/github/merged_prs.graphql): `SearchOpen`, `SearchMerged`, check mapping, rate limit and block classification.
- [server/internal/store/refresh.go](../../server/internal/store/refresh.go): `SaveRefresh`, `SaveSnapshot`, `LoadSnapshot`, `OpenPRs`, `UserLinks`, `SetUserLinks`, `SeedRules`. [repos.go](../../server/internal/store/repos.go): `Repos`.
- [server/internal/events/events.go](../../server/internal/events/events.go): the SSE `Hub`.
- [server/internal/api/handlers_snapshot.go](../../server/internal/api/handlers_snapshot.go): `GetSnapshot`, `SyncNow`.
- [server/cmd/prdash/run.go](../../server/cmd/prdash/run.go) and [accounts.go](../../server/cmd/prdash/accounts.go): wiring and the `Accounts` adapter.

## Key flows

1. Start: `Load` restores the stored snapshot (a corrupt one is ignored), then `Run` refreshes at once.
2. `Run` reads the settings, `plan` computes when GitHub and Jira are due (interval, doubled while the rate limit is low, or the backoff, whichever is later), runs a refresh when due and otherwise waits for the timer or `Reschedule`.
3. `refresh`: GitHub search (`SearchOpen`), merged search when PRs left, user links, Jira site, project keys, ticket lookup, repo states, `build`, encode and compress, `SaveRefresh` (one transaction), publish `snapshot` then `sync-status`.
4. A fetch failure never fails the refresh. GitHub rate limits and errors keep the last list and record `rate_limited` or `error` with `resetAt`. A rejected token or missing connection clears the list and records `disconnected`. Jira failures mark rows `ticketLookup: unreachable` and keep the last tickets.
5. Sync button: `POST /api/sync` calls `Trigger` (202, joins the running refresh). Connecting or disconnecting an account and changing the Jira site also call `Trigger`. Saving settings calls `Reschedule`.
6. Manual link changes: `PUT /api/prs/{id}/tickets` stores them and `Relink` rebuilds the snapshot from memory, asking Jira only for keys it has not seen ([ticket-linking.md](ticket-linking.md)).
7. Browser: `GET /api/snapshot` with `If-None-Match` answers `304` when the version is unchanged. `GET /events` streams `snapshot {version}` and `sync-status {running, githubAt, jiraAt, nextGithubAt, nextJiraAt}`.

## Outside calls

Per refresh, independent of the number of PRs:

- GitHub: one GraphQL search per 50 PRs (120 PRs are 3 requests), plus one merged search only when PRs left the open list. The same query returns the per-check CI.
- Jira (on the Jira interval): one project search page per 50 projects, one JQL per 100 ticket keys (120 keys are 2 requests). Between Jira intervals only keys not looked up before are asked.
- Backoff: GitHub rate limits wait for `Retry-After` or `resetAt`, other errors use `internal/backoff`. Jira honours `Retry-After` on `429` and `503`.

## Rules and invariants

- Lifecycle: `New` creates the base context of every refresh, so `Sync`, `Trigger` and `Relink` work before `Run`. When `Run`'s context ends it cancels that context, refuses new refreshes (a flight that is already done with `context.Canceled`; a later `Run` starts afresh) and returns only after the running refresh goroutine has finished (a `sync.WaitGroup`), so nothing touches the database after the server closes it.

- One refresh at a time. `Sync` and `Trigger` join the running one. `Relink` waits for it.
- Exactly one store transaction per refresh that fetched GitHub data. A refresh without a GitHub fetch writes only the snapshot row.
- The snapshot is immutable once published. `Current` returns an atomic pointer, and `Published.JSON` is encoded once and served as is.
- PRs that left the list are resolved only from a complete search: not capped, not partial, no GraphQL errors and no blocked organization. Otherwise they stay open.
- Tokens never enter the snapshot, the log or an event. Errors in the snapshot are fixed, user-facing texts.
- The SSE hub keeps the last event per name, replays it to a new client, drops a client whose buffer is full and sets a write deadline per write. `WriteTimeout` of the HTTP server stays unset.
- `go test -race` covers the single flight with concurrent `Sync` calls (exactly one outbound refresh).

## Tests

- [server/internal/poller/poller_test.go](../../server/internal/poller/poller_test.go) and [n1_test.go](../../server/internal/poller/n1_test.go): snapshot content and schema conformance, single flight, persistence and reload, one transaction per refresh, merged search rules, failures and recovery, interval adaptation, `Run` scheduling, relink, and the no-N+1 check with 120 PRs (3 GitHub requests, 2 Jira searches, 1 project call).
- [server/internal/events/events_test.go](../../server/internal/events/events_test.go): replay, heartbeat, slow client dropped, close.
- [server/internal/store/refresh_test.go](../../server/internal/store/refresh_test.go), [server/internal/github/search_test.go](../../server/internal/github/search_test.go).
- [server/internal/api/snapshot_api_test.go](../../server/internal/api/snapshot_api_test.go): ETag and 304, sync, SSE needs a session and heartbeats.
- [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts).
