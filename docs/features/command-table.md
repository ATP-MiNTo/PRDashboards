# Command Table

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 10.5 and 11. Related: [toolbar-pager-footer.md](toolbar-pager-footer.md), [sidebar-shell.md](sidebar-shell.md), [poller.md](poller.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [theme.md](theme.md).

## Purpose

The dashboard at `/`: one glass card per open pull request assigned to the user or opened by the user, with the merge method,
the merge panel inside the card, the Account panel, and the left sidebar with the Settings content ([sidebar-shell.md](sidebar-shell.md)).

Differences from the spec:

- The Merge column shows why a PR cannot be merged, computed in the browser from the snapshot (`blockOf`). The server checks the same things again when merging ([merge-and-transition.md](merge-and-transition.md)).
- There is no full-width top bar. Two floating pills ([header.md](header.md)) hold the burger that toggles the left sidebar with Settings ([sidebar-shell.md](sidebar-shell.md)), the search ([search.md](search.md)), Refresh and the theme circle. The source status moved into the popover of Refresh.
- The list is cards, not a table. Each card has the avatar, the title, a meta line (repository and number, author, the age of the last update and of the last commit), the Jira row with the size of the change (+added, -removed), and a controls row (Hide, Review, CI, Method, Merge) with dividers. The design draws other controls in that row; they come with the merge and review slices, and until then the card keeps the controls of the app.
- The stat strip is four tiles: Showing, Ready to merge, Running CI and Stacks. They count what the Show, hidden and search switches leave, and each tile is a button that filters the list to its pull requests ("Showing" lifts the filter). The status chips (All, Ready, Blocked, Draft, Stale) and the Blocked, Draft and Stale filters are gone; a PR's block reason still shows in its Merge cell. The sort, "Show" and "Hidden PRs" toolbar and the legend sit under the strip. Cards can be hidden, the list is paged at the bottom, and a footer with a GitHub button ends the page. All of it is client-side over the snapshot and is described in [toolbar-pager-footer.md](toolbar-pager-footer.md).
- Cards are tinted whole by why the PR is listed (`SnapshotPr.roles`): cyan for assigned only, red for opened by the user only, split 46/54 when both. A stack is one group with a purple left border and a banner ([stacked-prs.md](stacked-prs.md)). See the rules below.
- "Manage accounts", at the bottom of the sidebar, opens a side panel with the Connected accounts content. The Accounts page (`/accounts/`) stays for OAuth returns and shows the same content.

## Data model

Reads `Snapshot` from `GET /api/snapshot` ([poller.md](poller.md)), which gives each PR its `author`, `updatedAt`, `additions` and `deletions` (all nullable) for the card and `Settings.theme` (applied by `useTheme`; the theme is changed in Settings, [theme.md](theme.md)). Writes, through the ticket cell, `TicketLinks`. The Method column reads `SnapshotPr.mergeMethod` ([merge-method.md](merge-method.md)); the merge panel calls the merge API ([merge-and-transition.md](merge-and-transition.md)). Filter (a stat tile), sort, page, expanded, chosen method and Account panel state are component state only; the hidden pull requests, the hide mode and the page size are in `localStorage` ([toolbar-pager-footer.md](toolbar-pager-footer.md)); the outcome of merges made in the tab is in the query cache under `merges`.

## Code map

- [web/src/app/page.tsx](../../web/src/app/page.tsx): `/`, session check, then `CommandTable`.
- [web/src/components/CommandTable/CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): page layout (a flex row: the sidebar, then a main column with the header pills, the title bar, the mobile search field, the stat strip, the toolbar and the list), snapshot and status queries, the list view state from `useListPrefs`, the toolbar, the footer, whether the Account panel is open (`SidePanel`, [SidePanel.tsx](../../web/src/components/SidePanel/SidePanel.tsx)), and the sidebar state from `useSidebar` ([sidebar-shell.md](sidebar-shell.md)). The main column is a flex column with `min-height: 100dvh`; the content grows and the footer is the last child, so the page ends at the footer's bottom edge with no extra space.
- [web/src/components/Header/](../../web/src/components/Header/Header.tsx): the header, with the brand, search, Refresh (which calls `POST /api/sync` and carries the source status of GitHub and Jira, the sync times and the countdown in a popover) and the theme circle ([header.md](header.md)). The old `TopBar.tsx` is gone; its status content is `Header/SyncDetails.tsx`.
- [MethodCell.tsx](../../web/src/components/CommandTable/MethodCell.tsx), [MergePanel.tsx](../../web/src/components/CommandTable/MergePanel.tsx), [MergeSteps.tsx](../../web/src/components/CommandTable/MergeSteps.tsx), [RecentMerges.tsx](../../web/src/components/CommandTable/RecentMerges.tsx): see [merge-method.md](merge-method.md) and [merge-and-transition.md](merge-and-transition.md).
- [OrgBanner.tsx](../../web/src/components/CommandTable/OrgBanner.tsx): blocked organizations with an Authorize link, rate limit and error banners that say how old the shown data is, the warnings (for example the 1,000 result cap) and the disconnected notice.
- [StatStrip.tsx](../../web/src/components/CommandTable/StatStrip.tsx): the four filter tiles (`aria-pressed` buttons in a `Summary` group).
- [List.tsx](../../web/src/components/CommandTable/List.tsx): the cards, the stack groups, the sort, hide and paging pipeline, the ghost animations of hiding cards (`GHOST_MS`, `PULSE_MS`), the "Nothing here" message (`Notice`) and the pager under the list.
- [StackBanner.tsx](../../web/src/components/CommandTable/StackBanner.tsx): the line above a stack ([stacked-prs.md](stacked-prs.md)). [Icons.tsx](../../web/src/components/CommandTable/Icons.tsx): the small icons of the meta line and the stack.
- [web/src/components/Marquee/](../../web/src/components/Marquee/Marquee.tsx) and [web/src/lib/marquee.ts](../../web/src/lib/marquee.ts): a line cut with an ellipsis that slides to its end while the pointer is over it.
- [web/src/components/Toolbar/](../../web/src/components/Toolbar/): toolbar, `RollSwitch`, `Hint`, `Pager`, `Footer`, `useListPrefs` ([toolbar-pager-footer.md](toolbar-pager-footer.md)).
- [Card.tsx](../../web/src/components/CommandTable/Card.tsx): one card, subscribed to its own PR with `useSnapshotPr`, with the Hide checkbox at the start of the controls row (dimmed in All mode, animated while it changes). The meta line carries the "New repo" and "Last data" pills (the age of the data, `lastGoodGithub`, while GitHub is rate limited or failing), the Draft tag and the stack position. [web/src/lib/card.ts](../../web/src/lib/card.ts): `initials` and `ageShort` (8m, 3h, 2d).
- [CiCell.tsx](../../web/src/components/CommandTable/CiCell.tsx): rollup pill and per-check rows. [TicketCell.tsx](../../web/src/components/CommandTable/TicketCell.tsx): see [ticket-linking.md](ticket-linking.md).
- [useSnapshotEvents.ts](../../web/src/components/CommandTable/useSnapshotEvents.ts): `EventSource` client. [useNow.ts](../../web/src/components/CommandTable/useNow.ts) and [RelativeTime.tsx](../../web/src/components/CommandTable/RelativeTime.tsx): shared tickers.
- [CommandTable.module.css](../../web/src/components/CommandTable/CommandTable.module.css): styles on the tokens in [globals.css](../../web/src/app/globals.css). The stat tiles, the cards, the stack groups, the "Nothing here" message and the recent merges are glass panes (`composes: glass from global`). The title is cut at a third of the card's width (at least 140px) with container query units, so no script measures it.
- [web/src/lib/show.ts](../../web/src/lib/show.ts): the roles of a row and the Show filter ([toolbar-pager-footer.md](toolbar-pager-footer.md)).
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `blockOf`, `matches` and `groupsOf` (the `all`, `ready`, `running` and `stacks` filters), `safeLink`, URL builders and time helpers. [sort.ts](../../web/src/lib/sort.ts) and [view.ts](../../web/src/lib/view.ts) hold the sort, hide, paging and `listStats` logic.
- [web/src/api/queries.ts](../../web/src/api/queries.ts): `useSnapshot`, `useSnapshotPr`, `useSyncNow`, `useSaveTickets`, and the merge, repo and activity hooks.

## Key flows

1. `/` loads the session, then `getSnapshot` (sends `If-None-Match`, reuses the cached copy on `304`).
2. `useSnapshotEvents` opens `/events`. A `snapshot` event invalidates the query, a `sync-status` event is stored for the header.
3. `List` selects groups of ids from the snapshot (filter, Show and search, stacks in order, then sorted and with hidden pull requests applied) and shows the current page of them. TanStack Query's structural sharing keeps the result equal when no id or grouping changed, so only changed cards re-render: each `Card` selects its own PR.
4. The Merge button of a ready card toggles the merge panel inside the card, under its controls. The Method dropdown opens on hover (after a short delay), click or keyboard, lists the methods with the disallowed ones disabled, and shows the source and the fallback warning below.
5. Refresh (the circle in the header) calls `POST /api/sync`. It spins and ignores clicks while the status says running, and a toast says what the refresh found ([header.md](header.md)).
6. The CI column shows the rollup pill, then each check as a compact row: status dot, name, app (or the host for an external CI), duration. A check name links to its run page. When GitHub reports more checks than the 50 listed, "+N more" links to the PR's checks page.

## Outside calls

None from the browser to a provider. It calls only `/api/*` and `/events` on its own origin.

## Rules and invariants

- An enabled Merge button is the design's blue (`--merge` in [globals.css](../../web/src/app/globals.css), `#007fff`) with white text, the merge mark, radius .5rem, height 32px, and an aura that pulses for 2.6s (`aura` keyframes in `CommandTable.module.css`, off under `prefers-reduced-motion`). Hover is `--merge-hover`, a press scales to 0.95, the cursor is `merge.svg`. A disabled Merge button has the shared disabled look (20% fill and text) and the "no" cursor and no aura. See [theme.md](theme.md).
- The Merge button is enabled only for a ready row. The panel never sends a head SHA other than the one the row had when the panel opened.
- A card is a flex row from 1024px (the body takes the rest, the controls row has fixed columns with 1px dividers) and a column below it, so the controls stack under the title at 390px. Padding 16px, radius 1.1rem, gap 16px between cards. There is no hover state on a card.
- Links come from `safeLink` or the URL builders: https only, no credentials, github.com links rebuilt from the path alone, external hosts shown as host text, `target="_blank"` with `rel="noopener noreferrer"`. The Authorize link is built from a validated organization name (`https://github.com/orgs/<org>/sso`), never from a server URL.
- No inline styles (CSS Modules and custom properties), no `dangerouslySetInnerHTML`. `pnpm -C web build` writes the CSP hashes and must pass.
- Times are relative with the absolute time as tooltip. Animations stop under `prefers-reduced-motion`.
- Role cue: `Card` adds `role_assigned`, `role_authored` or `role_both` (from `roleKind`) to the card. The classes set the whole card's background and border from the `--card-a-*`, `--card-r-*` and `--card-b-*` tokens in globals.css: 28% of the design's `--rc-a` or `--rc-r` over the surface in light mode, the translucent teal and pink gradients of the design in dark mode, and a 46/54 split for both. Classes only, no `style` attribute. Muted text uses `--sub` (85% in light, 80% in dark) rather than the design's 70% and 60% opacity, so that it keeps 4.5:1 against every tint. Dimmed hidden cards keep their intentionally lower opacity.
- Hiding: in All mode a hidden card is at 50% opacity. A card that leaves the list plays `ghostOut` (.48s, fades, blurs, turns grey and moves up 16px) and is removed when it ends; one that stays plays `ghostDim` or `ghostIn` once (.5s). Under `prefers-reduced-motion` the change is applied at once.
- Ready, blocked and draft come from `blockOf`. Running CI is `ci.state === 'running'`.
- Per-check states: passing, failing, running, pending, skipped, neutral, cancelled.

## Tests

- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): cards, the stack group, the stat tiles and their filters, org banner, source banners, sync and events, theme applied, the Manage accounts flow and the sidebar accounts block, row isolation, method cell, merge panel toggle, the burger opening the Settings sidebar.
- [CiCell.test.tsx](../../web/src/components/CommandTable/CiCell.test.tsx): checks list, links, https only, "+N more".
- Role cues, Show switch and legend: [ListControls.test.tsx](../../web/src/components/CommandTable/ListControls.test.tsx), [web/src/lib/show.test.ts](../../web/src/lib/show.test.ts).
- [Cards.test.tsx](../../web/src/components/CommandTable/Cards.test.tsx): the card (avatar, meta line, ages, size of the change, missing data), the stack banner, the hide animations, the list messages. [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts), [card.test.ts](../../web/src/lib/card.test.ts), [marquee.test.ts](../../web/src/lib/marquee.test.ts).
- Sort, hide, paging and footer: [toolbar-pager-footer.md](toolbar-pager-footer.md) lists their tests, among them [ListControls.test.tsx](../../web/src/components/CommandTable/ListControls.test.tsx).
- [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): see [header.md](header.md).
- [MethodCell.test.tsx](../../web/src/components/CommandTable/MethodCell.test.tsx), [MergePanel.test.tsx](../../web/src/components/CommandTable/MergePanel.test.tsx).
- [e2e/dashboard.spec.ts](../../e2e/dashboard.spec.ts): SSO-blocked organization. [e2e/merge.spec.ts](../../e2e/merge.spec.ts): merge flows. [e2e/ratelimit.spec.ts](../../e2e/ratelimit.spec.ts): rate limit waiting state and recovery.
