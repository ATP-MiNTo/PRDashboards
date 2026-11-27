# Command Table

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 10.5 and 11. Related: [toolbar-pager-footer.md](toolbar-pager-footer.md), [sidebar-shell.md](sidebar-shell.md), [poller.md](poller.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [theme.md](theme.md).

## Purpose

The dashboard at `/`: one row per open pull request assigned to the user or opened by the user, with the merge method,
the merge panel under the row, the Account panel, and the left sidebar with the Settings content ([sidebar-shell.md](sidebar-shell.md)).

Differences from the spec:

- The Merge column shows why a PR cannot be merged, computed in the browser from the snapshot (`blockOf`). The server checks the same things again when merging ([merge-and-transition.md](merge-and-transition.md)).
- Settings is not in the top bar. A floating burger button toggles the left sidebar that holds it ([sidebar-shell.md](sidebar-shell.md)).
- Under the stat strip the status chips (All, Ready, Blocked, Draft, Stale) stay, with the sort, "Show" and "Hidden PRs" toolbar and the legend next to them in the same row. Rows can be hidden, the list is paged at the bottom of the table, and a footer with a GitHub button ends the page. All of it is client-side over the snapshot and is described in [toolbar-pager-footer.md](toolbar-pager-footer.md).
- Rows are tinted by why the PR is listed (`SnapshotPr.roles`): cyan for assigned only, pink for opened by the user only, split half and half when both. A stack keeps its purple cue (border and header). See the rules below.
- The Account button opens a side panel with the Connected accounts content. The Accounts page (`/accounts/`) stays for OAuth returns and shows the same content.

## Data model

Reads `Snapshot` from `GET /api/snapshot` ([poller.md](poller.md)) and `Settings.theme` (applied by `useTheme`; the theme is changed in Settings, [theme.md](theme.md)). Writes, through the ticket cell, `TicketLinks`. The Method column reads `SnapshotPr.mergeMethod` ([merge-method.md](merge-method.md)); the merge panel calls the merge API ([merge-and-transition.md](merge-and-transition.md)). Filter, sort, page, expanded, chosen method and Account panel state are component state only; the hidden pull requests, the hide mode and the page size are in `localStorage` ([toolbar-pager-footer.md](toolbar-pager-footer.md)); the outcome of merges made in the tab is in the query cache under `merges`.

## Code map

- [web/src/app/page.tsx](../../web/src/app/page.tsx): `/`, session check, then `CommandTable`.
- [web/src/components/CommandTable/CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): page layout (a flex row: the sidebar, then a main column with the burger, the top bar and the table), snapshot and status queries, the list view state from `useListPrefs`, the toolbar, the footer, whether the Account panel is open (`SidePanel`, [SidePanel.tsx](../../web/src/components/SidePanel/SidePanel.tsx)), and the sidebar state from `useSidebar` ([sidebar-shell.md](sidebar-shell.md)). The main column is a flex column with `min-height: 100dvh`; the content grows and the footer is the last child, so the page ends at the footer's bottom edge with no extra space.
- [TopBar.tsx](../../web/src/components/CommandTable/TopBar.tsx): brand ("PR Dashboards" followed by a decorative repo and branch icon, and a note; it fades out while the floating pill shows the name), source status of GitHub and Jira (`sourceLabel`, with the time left of a rate limit), sync times, countdown, Sync button, and Account button (opens the Account panel, `aria-haspopup="dialog"`). There is no Settings button and no theme control in the bar.
- [MethodCell.tsx](../../web/src/components/CommandTable/MethodCell.tsx), [MergePanel.tsx](../../web/src/components/CommandTable/MergePanel.tsx), [MergeSteps.tsx](../../web/src/components/CommandTable/MergeSteps.tsx), [RecentMerges.tsx](../../web/src/components/CommandTable/RecentMerges.tsx): see [merge-method.md](merge-method.md) and [merge-and-transition.md](merge-and-transition.md).
- [OrgBanner.tsx](../../web/src/components/CommandTable/OrgBanner.tsx): blocked organizations with an Authorize link, rate limit and error banners that say how old the shown data is, the warnings (for example the 1,000 result cap) and the disconnected notice.
- [StatStrip.tsx](../../web/src/components/CommandTable/StatStrip.tsx): `StatStrip` and `Filters`.
- [Table.tsx](../../web/src/components/CommandTable/Table.tsx): columns, one `tbody` per group, stack header, the sort, hide and paging pipeline, the fade-out of hiding rows, and the pager under the table.
- [web/src/components/Toolbar/](../../web/src/components/Toolbar/): toolbar, `RollSwitch`, `Hint`, `Pager`, `Footer`, `useListPrefs` ([toolbar-pager-footer.md](toolbar-pager-footer.md)).
- [Row.tsx](../../web/src/components/CommandTable/Row.tsx): one row, subscribed to its own PR with `useSnapshotPr`, with the Hide checkbox in the first cell (dimmed in All mode, fading while it leaves). While GitHub is rate limited or failing, a "Last data" mark with the age of the data (`lastGoodGithub`) shows next to the repo.
- [CiCell.tsx](../../web/src/components/CommandTable/CiCell.tsx): rollup pill and per-check rows. [TicketCell.tsx](../../web/src/components/CommandTable/TicketCell.tsx): see [ticket-linking.md](ticket-linking.md).
- [useSnapshotEvents.ts](../../web/src/components/CommandTable/useSnapshotEvents.ts): `EventSource` client. [useNow.ts](../../web/src/components/CommandTable/useNow.ts) and [RelativeTime.tsx](../../web/src/components/CommandTable/RelativeTime.tsx): shared tickers.
- [CommandTable.module.css](../../web/src/components/CommandTable/CommandTable.module.css): styles on the palette tokens in [globals.css](../../web/src/app/globals.css).
- [web/src/lib/show.ts](../../web/src/lib/show.ts): the roles of a row and the Show filter ([toolbar-pager-footer.md](toolbar-pager-footer.md)).
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `blockOf`, `statsOf`, `groupsOf`, `safeLink`, URL builders and time helpers. [sort.ts](../../web/src/lib/sort.ts) and [view.ts](../../web/src/lib/view.ts) hold the sort, hide and paging logic.
- [web/src/api/queries.ts](../../web/src/api/queries.ts): `useSnapshot`, `useSnapshotPr`, `useSyncNow`, `useSaveTickets`, and the merge, repo and activity hooks.

## Key flows

1. `/` loads the session, then `getSnapshot` (sends `If-None-Match`, reuses the cached copy on `304`).
2. `useSnapshotEvents` opens `/events`. A `snapshot` event invalidates the query, a `sync-status` event is stored for the top bar.
3. `Table` selects groups of ids from the snapshot (filter, stacks in order, then sorted and with hidden pull requests applied) and shows the current page of them. TanStack Query's structural sharing keeps the result equal when no id or grouping changed, so only changed rows re-render: each `Row` selects its own PR.
4. The Merge button of a ready row toggles the merge panel in a row under it. The Method dropdown opens on hover (after a short delay), click or keyboard, lists the methods with the disallowed ones disabled, and shows the source and the fallback warning below.
5. Sync calls `POST /api/sync`. The button stays disabled while the status says running.
6. The CI column shows the rollup pill, then each check as a compact row: status dot, name, app (or the host for an external CI), duration. A check name links to its run page. When GitHub reports more checks than the 50 listed, "+N more" links to the PR's checks page.

## Outside calls

None from the browser to a provider. It calls only `/api/*` and `/events` on its own origin.

## Rules and invariants

- An enabled Merge button shows a coloured aura (box-shadow) from the `--merge-aura` custom property in `CommandTable.module.css`; disabled buttons show none.
- The Merge button is enabled only for a ready row. The panel never sends a head SHA other than the one the row had when the panel opened.
- Columns, in order: Repo / PR, Ticket, Last commit, Review, CI, Method, Merge. The table has a minimum width of 860px and 8px cell padding and the parts of a CI check row wrap, so that it fits beside the 24rem sidebar at 1280px and 1440px without scrolling sideways (an unbroken check name or host can still force it); below that it scrolls inside its box.
- Links come from `safeLink` or the URL builders: https only, no credentials, github.com links rebuilt from the path alone, external hosts shown as host text, `target="_blank"` with `rel="noopener noreferrer"`. The Authorize link is built from a validated organization name (`https://github.com/orgs/<org>/sso`), never from a server URL.
- No inline styles (CSS Modules and custom properties), no `dangerouslySetInnerHTML`. `pnpm -C web build` writes the CSP hashes and must pass.
- Times are relative with the absolute time as tooltip. Animations stop under `prefers-reduced-motion`.
- Role cue: `Row` adds `role_assigned`, `role_authored` or `role_both` (from `roleKind`) to the row. The classes set a soft background from the `--role-assigned-*` and `--role-authored-*` tokens in globals.css (light and dark values), a darker hover, and a 4px bar on the first cell; `role_both` splits the background left and right and the bar top and bottom. Classes only, no `style` attribute. Text contrast against every tint, hover included, is at least 4.5:1 in the light (winter) and dark (night) modes (body text 10.6:1 or more, muted text 4.7:1 or more). Dimmed hidden rows keep their intentionally lower opacity.
- A PR is stale when its last commit is older than 3 days. Ready, blocked and draft come from `blockOf`.
- Per-check states: passing, failing, running, pending, skipped, neutral, cancelled.

## Tests

- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): columns and rows, stats and filters, org banner, source banners, sync and events, theme applied, Account panel, row isolation, method cell, merge panel toggle, the burger opening the Settings sidebar.
- [CiCell.test.tsx](../../web/src/components/CommandTable/CiCell.test.tsx): checks list, links, https only, "+N more".
- Role cues, Show switch and legend: [ListControls.test.tsx](../../web/src/components/CommandTable/ListControls.test.tsx), [web/src/lib/show.test.ts](../../web/src/lib/show.test.ts).
- [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts).
- Sort, hide, paging and footer: [toolbar-pager-footer.md](toolbar-pager-footer.md) lists their tests, among them [ListControls.test.tsx](../../web/src/components/CommandTable/ListControls.test.tsx).
- [TopBar.test.tsx](../../web/src/components/CommandTable/TopBar.test.tsx): source status, the rate limit countdown, the Account button, the brand name followed by its icon, no Settings button, no theme control.
- [MethodCell.test.tsx](../../web/src/components/CommandTable/MethodCell.test.tsx), [MergePanel.test.tsx](../../web/src/components/CommandTable/MergePanel.test.tsx).
- [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts): SSO-blocked organization. [e2e/merge.spec.ts](../../e2e/merge.spec.ts): merge flows. [e2e/ratelimit.spec.ts](../../e2e/ratelimit.spec.ts): rate limit waiting state and recovery.
