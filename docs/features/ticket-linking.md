# Ticket linking

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 7.1 and 10.3. Related: [poller.md](poller.md), [command-table.md](command-table.md), [auth.md](auth.md).

## Purpose

Links each pull request to Jira tickets from its branch and title, lets the user add or hide tickets,
and shows each ticket with its status. Transitions are read (cached) but nothing moves a ticket yet.

## Data model

- `ticket_links(pr_id, ticket_key, source, removed, position)`: automatic links are rewritten by every refresh, manual links (`source = manual`) and hidden keys (`removed = 1`) are written only by `SetUserLinks`.
- API schemas: `SnapshotTicket`, `TicketLookup` (`ok`, `unverified`, `unreachable`), the `waiting` hint of a ticket with an All rule that waits ([status-sync-rules.md](status-sync-rules.md)), `TicketLinks`, `TicketSummary`, `AssignedTickets`, `Transitions`.
- Setting read: `jiraSiteId` (or the only site).

## Code map

- [server/internal/linking/linking.go](../../server/internal/linking/linking.go): pure `Resolve`, `Keys`, `Index`, `AllKeys`, `Projects`.
- [server/internal/jira/jira.go](../../server/internal/jira/jira.go): `Client` with `Search`, `Projects`, `Assigned`, `Transitions`.
- [server/internal/poller/refresh.go](../../server/internal/poller/refresh.go): site, project keys and ticket lookup. [build.go](../../server/internal/poller/build.go) adds tickets to rows.
- [server/internal/api/handlers_snapshot.go](../../server/internal/api/handlers_snapshot.go): `PutPrTickets`, `GetAssignedTickets`, `GetTransitions`.
- [server/internal/fake/jira.go](../../server/internal/fake/jira.go): fake Jira for the e2e build.
- [web/src/components/CommandTable/TicketCell.tsx](../../web/src/components/CommandTable/TicketCell.tsx): the Ticket cell.

## Key flows

1. Refresh: `resolve` runs `linking.Resolve` per PR (branch keys, then title keys, in order, deduplicated; manual links added; hidden keys removed; keys kept only when their project exists). Project keys come from one paginated `project/search` per Jira refresh. When the project list is unknown (Jira not connected) all keys are kept and the row is `unverified`.
2. `lookupTickets` asks Jira for all distinct keys, `key in (...)` in chunks of 100. Keys Jira does not return are marked not found.
3. Between Jira intervals, only keys never looked up are asked, so a new PR does not wait for the interval.
4. Jira unreachable: rows keep their last tickets and get `ticketLookup: unreachable`. The cell shows "Jira unreachable, retry the lookup" and a Retry lookup button, which calls `POST /api/sync`.
5. Unlink (x): an automatic ticket is hidden (`removed`), a manual one is dropped. Link ticket opens the picker, which lists `GET /api/tickets/assigned` (searchable, minus already linked tickets). Both call `PUT /api/prs/{id}/tickets` with the whole desired `{manual, removed}`, then `Relink` rebuilds the snapshot from memory.
6. `GET /api/tickets/{key}/transitions` returns the moves with their required fields, cached 60 s per ticket.

## Outside calls

- `POST /rest/api/3/search/jql`, `key in (...)`, 100 keys per request, fields `summary,status,project,issuetype`.
- `GET /rest/api/3/project/search`, paginated, once per Jira refresh.
- `POST /rest/api/3/search/jql` with `assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC`, at most 100, cached for the Jira interval, only when the picker is opened.
- `GET /rest/api/3/issue/{key}/transitions?expand=transitions.fields`, cached 60 s, only on request.
- All through `netguard` to `https://api.atlassian.com/ex/jira/{cloudId}`, with the OAuth access token (refreshed by `auth`). `429` and `503` back off with `Retry-After`.

## Rules and invariants

- `linking` is pure: no network, database or clock.
- Keys sent to Jira, and keys accepted by `PUT /api/prs/{id}/tickets`, match `^[A-Z][A-Z0-9]+-[0-9]+$`. At most 20 manual and 20 removed keys, no key in both lists or twice, all in one transaction. An unknown PR is `404`.
- Ticket links are built from the Jira site URL and the key, and the UI shows them only when https.
- A ticket may belong to many PRs and a PR may have many tickets.

## Tests

- [server/internal/linking/linking_test.go](../../server/internal/linking/linking_test.go): table tests, 100 % coverage.
- [server/internal/jira/jira_test.go](../../server/internal/jira/jira_test.go): recorded fixtures for search, missing keys, pagination, projects, assigned cache, transitions cache, backoff.
- [server/internal/poller/poller_test.go](../../server/internal/poller/poller_test.go): unreachable and unverified rows, relink, incremental lookup.
- [server/internal/api/snapshot_api_test.go](../../server/internal/api/snapshot_api_test.go): the three handlers.
- [web/src/components/CommandTable/TicketCell.test.tsx](../../web/src/components/CommandTable/TicketCell.test.tsx): unlink, picker, expand, error and retry.
