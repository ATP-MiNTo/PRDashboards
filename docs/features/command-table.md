# Command Table

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 10.5 and 11. Related: [poller.md](poller.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [theme.md](theme.md).

## Purpose

The dashboard at `/`: one row per open pull request assigned to the user, read only. Merge, Method,
the settings drawer and the merge panel are placeholders until milestones 6 and 7.

Differences from the spec:

- The Method column shows "Default" and the Merge button is disabled. The Merge column shows why a PR cannot be merged, computed in the browser from the snapshot (`blockOf`). The server enforces the same rules in milestone 6.
- Hover dropdowns of the prototype are not built, because no dropdown exists yet.
- The Settings button is disabled. The Accounts page (`/accounts/`) holds the Connected accounts panel.

## Data model

Reads `Snapshot` from `GET /api/snapshot` ([poller.md](poller.md)) and `Settings.theme`. Writes `Settings.theme` and, through the ticket cell, `TicketLinks`. Filter and expanded state are component state only.

## Code map

- [web/src/app/page.tsx](../../web/src/app/page.tsx): `/`, session check, then `CommandTable`.
- [web/src/components/CommandTable/CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): page layout, snapshot and status queries, filter state.
- [TopBar.tsx](../../web/src/components/CommandTable/TopBar.tsx): brand, theme switch, sync times, countdown, Sync button, Accounts link, Settings placeholder.
- [OrgBanner.tsx](../../web/src/components/CommandTable/OrgBanner.tsx): blocked organizations with an Authorize link, rate limit, error and warning banners, disconnected notice.
- [StatStrip.tsx](../../web/src/components/CommandTable/StatStrip.tsx): `StatStrip` and `Filters`.
- [Table.tsx](../../web/src/components/CommandTable/Table.tsx): columns, one `tbody` per group, stack header.
- [Row.tsx](../../web/src/components/CommandTable/Row.tsx): one row, subscribed to its own PR with `useSnapshotPr`.
- [CiCell.tsx](../../web/src/components/CommandTable/CiCell.tsx): rollup pill and per-check rows. [TicketCell.tsx](../../web/src/components/CommandTable/TicketCell.tsx): see [ticket-linking.md](ticket-linking.md).
- [useSnapshotEvents.ts](../../web/src/components/CommandTable/useSnapshotEvents.ts): `EventSource` client. [useNow.ts](../../web/src/components/CommandTable/useNow.ts) and [RelativeTime.tsx](../../web/src/components/CommandTable/RelativeTime.tsx): shared tickers.
- [CommandTable.module.css](../../web/src/components/CommandTable/CommandTable.module.css): styles on the palette tokens in [globals.css](../../web/src/app/globals.css).
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `blockOf`, `statsOf`, `groupsOf`, `safeLink`, URL builders and time helpers.
- [web/src/api/queries.ts](../../web/src/api/queries.ts): `useSnapshot`, `useSnapshotPr`, `useSyncNow`, `useSaveTickets`.

## Key flows

1. `/` loads the session, then `getSnapshot` (sends `If-None-Match`, reuses the cached copy on `304`).
2. `useSnapshotEvents` opens `/events`. A `snapshot` event invalidates the query, a `sync-status` event is stored for the top bar.
3. `Table` selects stack-ordered groups of ids from the snapshot. TanStack Query's structural sharing keeps the result equal when no id or grouping changed, so only changed rows re-render: each `Row` selects its own PR.
4. Sync calls `POST /api/sync`. The button stays disabled while the status says running.
5. The CI column shows the rollup pill, then each check as a compact row: status dot, name, app (or the host for an external CI), duration. A check name links to its run page. When GitHub reports more checks than the 50 listed, "+N more" links to the PR's checks page.

## Outside calls

None from the browser to a provider. It calls only `/api/*` and `/events` on its own origin.

## Rules and invariants

- Columns, in order: Repo / PR, Ticket, Last commit, Review, CI, Method, Merge.
- Links come from `safeLink` or the URL builders: https only, no credentials, github.com links rebuilt from the path alone, external hosts shown as host text, `target="_blank"` with `rel="noopener noreferrer"`. The Authorize link is built from a validated organization name (`https://github.com/orgs/<org>/sso`), never from a server URL.
- No inline styles (CSS Modules and custom properties), no `dangerouslySetInnerHTML`. `pnpm -C web build` writes the CSP hashes and must pass.
- Times are relative with the absolute time as tooltip. Animations stop under `prefers-reduced-motion`.
- A PR is stale when its last commit is older than 3 days. Ready, blocked and draft come from `blockOf`.
- Per-check states: passing, failing, running, pending, skipped, neutral, cancelled.

## Tests

- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): columns and rows, stats and filters, org banner, source banners, sync and events, theme, row isolation.
- [CiCell.test.tsx](../../web/src/components/CommandTable/CiCell.test.tsx): checks list, links, https only, "+N more".
- [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts).
- [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts): SSO-blocked organization.
