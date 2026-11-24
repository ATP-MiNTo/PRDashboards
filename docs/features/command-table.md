# Command Table

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 10.5 and 11. Related: [poller.md](poller.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [theme.md](theme.md).

## Purpose

The dashboard at `/`: one row per open pull request assigned to the user, with the merge method,
the merge panel under the row and the settings drawer.

Differences from the spec:

- The Merge column shows why a PR cannot be merged, computed in the browser from the snapshot (`blockOf`). The server checks the same things again when merging ([merge-and-transition.md](merge-and-transition.md)).
- The Accounts page (`/accounts/`) still holds the Connected accounts panel, which the settings drawer also shows.

## Data model

Reads `Snapshot` from `GET /api/snapshot` ([poller.md](poller.md)) and `Settings.theme`. Writes `Settings.theme` and, through the ticket cell, `TicketLinks`. The Method column reads `SnapshotPr.mergeMethod` ([merge-method.md](merge-method.md)); the merge panel calls the merge API ([merge-and-transition.md](merge-and-transition.md)). Filter, expanded, chosen method and open panel state are component state only; the outcome of merges made in the tab is in the query cache under `merges`.

## Code map

- [web/src/app/page.tsx](../../web/src/app/page.tsx): `/`, session check, then `CommandTable`.
- [web/src/components/CommandTable/CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): page layout, snapshot and status queries, filter state.
- [TopBar.tsx](../../web/src/components/CommandTable/TopBar.tsx): brand, theme switch, source status of GitHub and Jira (`sourceLabel`, with the time left of a rate limit), sync times, countdown, Sync button, Accounts link, Settings button ([settings.md](settings.md)).
- [MethodCell.tsx](../../web/src/components/CommandTable/MethodCell.tsx), [MergePanel.tsx](../../web/src/components/CommandTable/MergePanel.tsx), [MergeSteps.tsx](../../web/src/components/CommandTable/MergeSteps.tsx), [RecentMerges.tsx](../../web/src/components/CommandTable/RecentMerges.tsx): see [merge-method.md](merge-method.md) and [merge-and-transition.md](merge-and-transition.md).
- [OrgBanner.tsx](../../web/src/components/CommandTable/OrgBanner.tsx): blocked organizations with an Authorize link, rate limit and error banners that say how old the shown data is, the warnings (for example the 1,000 result cap) and the disconnected notice.
- [StatStrip.tsx](../../web/src/components/CommandTable/StatStrip.tsx): `StatStrip` and `Filters`.
- [Table.tsx](../../web/src/components/CommandTable/Table.tsx): columns, one `tbody` per group, stack header.
- [Row.tsx](../../web/src/components/CommandTable/Row.tsx): one row, subscribed to its own PR with `useSnapshotPr`. While GitHub is rate limited or failing, a "Last data" mark with the age of the data (`lastGoodGithub`) shows next to the repo.
- [CiCell.tsx](../../web/src/components/CommandTable/CiCell.tsx): rollup pill and per-check rows. [TicketCell.tsx](../../web/src/components/CommandTable/TicketCell.tsx): see [ticket-linking.md](ticket-linking.md).
- [useSnapshotEvents.ts](../../web/src/components/CommandTable/useSnapshotEvents.ts): `EventSource` client. [useNow.ts](../../web/src/components/CommandTable/useNow.ts) and [RelativeTime.tsx](../../web/src/components/CommandTable/RelativeTime.tsx): shared tickers.
- [CommandTable.module.css](../../web/src/components/CommandTable/CommandTable.module.css): styles on the palette tokens in [globals.css](../../web/src/app/globals.css).
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `blockOf`, `statsOf`, `groupsOf`, `safeLink`, URL builders and time helpers.
- [web/src/api/queries.ts](../../web/src/api/queries.ts): `useSnapshot`, `useSnapshotPr`, `useSyncNow`, `useSaveTickets`, and the merge, repo and activity hooks.

## Key flows

1. `/` loads the session, then `getSnapshot` (sends `If-None-Match`, reuses the cached copy on `304`).
2. `useSnapshotEvents` opens `/events`. A `snapshot` event invalidates the query, a `sync-status` event is stored for the top bar.
3. `Table` selects stack-ordered groups of ids from the snapshot. TanStack Query's structural sharing keeps the result equal when no id or grouping changed, so only changed rows re-render: each `Row` selects its own PR.
4. The Merge button of a ready row toggles the merge panel in a row under it. The Method dropdown opens on hover (after a short delay), click or keyboard, lists the methods with the disallowed ones disabled, and shows the source and the fallback warning below.
5. Sync calls `POST /api/sync`. The button stays disabled while the status says running.
6. The CI column shows the rollup pill, then each check as a compact row: status dot, name, app (or the host for an external CI), duration. A check name links to its run page. When GitHub reports more checks than the 50 listed, "+N more" links to the PR's checks page.

## Outside calls

None from the browser to a provider. It calls only `/api/*` and `/events` on its own origin.

## Rules and invariants

- The Merge button is enabled only for a ready row. The panel never sends a head SHA other than the one the row had when the panel opened.
- Columns, in order: Repo / PR, Ticket, Last commit, Review, CI, Method, Merge.
- Links come from `safeLink` or the URL builders: https only, no credentials, github.com links rebuilt from the path alone, external hosts shown as host text, `target="_blank"` with `rel="noopener noreferrer"`. The Authorize link is built from a validated organization name (`https://github.com/orgs/<org>/sso`), never from a server URL.
- No inline styles (CSS Modules and custom properties), no `dangerouslySetInnerHTML`. `pnpm -C web build` writes the CSP hashes and must pass.
- Times are relative with the absolute time as tooltip. Animations stop under `prefers-reduced-motion`.
- A PR is stale when its last commit is older than 3 days. Ready, blocked and draft come from `blockOf`.
- Per-check states: passing, failing, running, pending, skipped, neutral, cancelled.

## Tests

- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): columns and rows, stats and filters, org banner, source banners, sync and events, theme, row isolation, method cell, merge panel toggle, settings button.
- [CiCell.test.tsx](../../web/src/components/CommandTable/CiCell.test.tsx): checks list, links, https only, "+N more".
- [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts).
- [TopBar.test.tsx](../../web/src/components/CommandTable/TopBar.test.tsx): source status and the rate limit countdown.
- [MethodCell.test.tsx](../../web/src/components/CommandTable/MethodCell.test.tsx), [MergePanel.test.tsx](../../web/src/components/CommandTable/MergePanel.test.tsx).
- [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts): SSO-blocked organization. [e2e/merge.spec.ts](../../e2e/merge.spec.ts): merge flows. [e2e/ratelimit.spec.ts](../../e2e/ratelimit.spec.ts): rate limit waiting state and recovery.
