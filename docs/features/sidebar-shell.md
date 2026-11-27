# Sidebar shell

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [settings.md](settings.md), [command-table.md](command-table.md), [theme.md](theme.md), [app-shell.md](app-shell.md).

## Purpose

The left sidebar that holds the Settings content, and the accounts block pinned at its bottom. The burger that toggles it is in the left pill of the header ([header.md](header.md)). On a wide
screen (min-width 1024px) the sidebar is a column beside the page and is open by default; on a narrow
screen it is a drawer over the page, with a scrim, that opens from the burger. This is part of the v2 UI migration; the PRD and spec describe a Settings button in
the top bar, which no longer exists.

The sidebar is a glass pane 20rem (320px) wide with 20px padding, a 16px gap between its blocks, a radius of 0 20px 20px 0 and a hidden scrollbar. Its heading is a 36px gear square and "Settings" in Space Grotesk 18px bold, with the collapse chevron at the right. The Settings content follows ([settings.md](settings.md): Repositories, GitHub and Jira polling, and the Status sync rules button; the theme is not here, it is the header button ([theme.md](theme.md))), and the accounts block ends the pane. The content is one grid column that may shrink (`minmax(0, 1fr)`), so a long repository name is cut with an ellipsis (it slides on hover, [Marquee](../../web/src/components/Marquee/Marquee.tsx)) and never widens the pane: the VIA badge and the gear of a row, the default merge method select and Select all and Clear always stay inside it. (An `auto` track is as wide as the longest unbreakable line inside, which pushed these out of the sidebar's hidden overflow.)

## Data model

No server data. Browser storage: `localStorage` key `prd-side` (`1` open, `0` folded), written only
when the user toggles the sidebar on a wide screen. Every read and write is in `try/catch`; when
storage is blocked the choice lasts for the visit. The overlay state on a narrow screen and the
auto-collapse are not saved. Scroll position and the wide-screen media query are read from the window.

## Code map

- [web/src/components/Shell/useSidebar.ts](../../web/src/components/Shell/useSidebar.ts): `useSidebar({ hold })` (wide or narrow, open state, `toggle`, `close`, the `aside` ref, auto-collapse) and `useScrolled(threshold = 24)`. Constants `SIDEBAR_KEY`, `WIDE_QUERY`, `AUTO_COLLAPSE_MS`.
- [web/src/components/Shell/idleTimer.ts](../../web/src/components/Shell/idleTimer.ts): `createIdleTimer`, the pure one-shot timer with `arm`, `cancel`, `isArmed` and an `isHeld` check.
- [web/src/components/Shell/Sidebar.tsx](../../web/src/components/Shell/Sidebar.tsx): the frosted `aside` with the gear, the "Settings" title and the collapse chevron, as a column or as an overlay with a scrim. It takes the body as children and a `footer` that stays below the scrolling body.
- [SidebarAccounts.tsx](../../web/src/components/Shell/SidebarAccounts.tsx): the ACCOUNTS block (see Key flows). [BrandIcon.tsx](../../web/src/components/Shell/BrandIcon.tsx): the decorative repo and branch icon, used by the header. The burger itself is in [Header.tsx](../../web/src/components/Header/Header.tsx).
- [web/src/components/Shell/Shell.module.css](../../web/src/components/Shell/Shell.module.css): glass look from the glass tokens in [theme.md](theme.md) (pane colour, border, blur and shadow), the drawer transition, the scrim and the accounts block. The collapse chevron is a `ui-ghost` button (32px, 20% ink on hover).
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): wires the hook, the header with its burger, and the sidebar with `SettingsContent` ([settings.md](settings.md)) and `SidebarAccounts`.

## Key flows

1. Mount: `useSidebar` reads `prd-side` and the `(min-width: 1024px)` media query (through `useSyncExternalStore`, so the first render matches the static export: narrow and closed). On a wide screen the sidebar is open unless `prd-side` is `0`.
2. The burger and the chevron call `toggle` and `close`. On a wide screen this saves `prd-side`. On a narrow screen it only changes the overlay.
3. Overlay: the `aside` is `role="dialog"` with `aria-modal`, focus moves into it, Tab wraps inside, Escape or a click on the scrim closes it, and focus returns to the burger. On a wide screen the `aside` is a plain labelled region and takes no focus.
4. Auto-collapse (wide, open): a 30 second `createIdleTimer` starts on mount and whenever the pointer is outside the sidebar. Entering the sidebar (mouse move) cancels it, and so does keyboard focus moving into it; leaving the sidebar or the window starts it again. When it fires and keyboard focus is inside the sidebar, `hold` is true or any `role="dialog"` with `aria-modal="true"` is open over the page (the repository settings, the Accounts, a note, the rules), it starts over instead; otherwise the sidebar folds without saving. Focus counts only as the keyboard's: the hook tracks the last input (a key press other than a modifier sets it, a pointer down or any mouse move clears it). A button the pointer clicked keeps the focus after the pointer has left, and treating that as a hold kept the sidebar open for good, so it holds nothing.
5. Drawer: the `aside` is always mounted. `data-open` says whether it is open and the slide is a `transform` transition of .3s (`cubic-bezier(0, 0, .2, 1)`) on a narrow screen. The scrim fades in over .2s. While it is closed the `aside` is `inert` and `aria-hidden`, and its `visibility` turns hidden after the .3s, so nothing in it takes focus or is read. On the first render nothing animates (`data-animate` is off until the state has been read from the browser). Transitions are off with `prefers-reduced-motion: reduce`.
6. Accounts block: `SidebarAccounts` reads `GET /api/accounts` and shows an ACCOUNTS eyebrow, a GitHub row (the primary token's login, and a badge: Token, OAuth or GitHub App, plus Reconnect when a token needs it; the tokens themselves are listed in the Accounts dialog, [auth.md](auth.md)) and a Jira row (site name, Atlassian, Reconnect or Not connected). "Manage accounts" (`aria-haspopup="dialog"`) opens the Accounts dialog ([auth.md](auth.md)), which replaces the Account button of the old top bar and the right-hand side panel. The 30 second auto-collapse does not fold the sidebar while a dialog is open. The ACCOUNTS eyebrow row also carries, at its right, the GitHub API usage of the design: "API 4180/5000" (`ApiUsage` in SidebarAccounts.tsx; 0.75rem, tabular numbers, 60% opacity). It reads `sources.github.rateLimit` (`used`, `limit`) from the snapshot the page already has, so it makes no request. The visible text is `aria-hidden` and a visually hidden "GitHub API usage 4180 of 5000" is read instead. Nothing is shown (no placeholder) when the snapshot has no rate limit: before the first refresh, when GitHub is disconnected, or for a snapshot stored by an older version. See [github-tokens.md](github-tokens.md) for the several tokens.

## Outside calls

None from the shell. The accounts block reads `GET /api/accounts`, which reads the local database and makes no GitHub or Jira call. The sidebar content mounts when the sidebar first opens (page load on a wide screen) and stays mounted after a collapse (hidden with the `hidden` attribute). On mount it runs only local queries (settings, repos); the rules, the activity and the Jira options request, the one that reaches Jira, wait until the Status sync rules dialog opens ([settings.md](settings.md), [status-sync-rules.md](status-sync-rules.md)). Browser tabs therefore make no outside call by loading the dashboard.

## Rules and invariants

- Classes only: no `style` attribute and no `<style>` block, because the CSP is `style-src 'self'` ([security.md](security.md)). Dynamic state is a class or a data attribute.
- The blur is on a `::before` layer of the sidebar, not on the sidebar: `backdrop-filter` on an ancestor would become the containing block of the fixed popovers in the Dropdown.
- The burger ([header.md](header.md)) and the collapse chevron are buttons with accessible names ("Settings sidebar", "Collapse sidebar") and `aria-expanded`; both have `aria-controls`.
- The sidebar column is 20rem wide, and the overlay is at most 20rem or 92vw. The table is sized to fit beside it at 1280px ([command-table.md](command-table.md)).
- The Accounts dialog is not part of the sidebar; the sidebar only opens it. Sessions, Exclude statuses, the per-rule Multi-PR mode and Activity are not in the sidebar any more (the Repositories block keeps the global Delete branch and Multi-PR defaults) ([settings.md](settings.md)).

## Tests

- [Shell.test.tsx](../../web/src/components/Shell/Shell.test.tsx): open and close from a burger and the chevron, saved and restored state, blocked storage, overlay (dialog, Escape, scrim, focus return, Tab wrap), auto-collapse with fake timers (folds after 30s; waits while the pointer is inside, while keyboard focus is inside, while a dialog is open and 30s after it closed; folds although a clicked button still has the focus, and after a mouse move that follows a key press).
- [SettingsContent.test.tsx](../../web/src/components/Settings/SettingsContent.test.tsx) and [RulesModal.test.tsx](../../web/src/components/RulesModal/RulesModal.test.tsx): no Jira options request from the sidebar, one when the rules dialog opens. [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): the "Status sync rules" button opens the dialog and the focus returns to it on close.
- [idleTimer.test.ts](../../web/src/components/Shell/idleTimer.test.ts): fire, cancel, restart, held.
- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx) and [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): the burger opens the sidebar, no Settings button, the accounts block with its badges, the API usage text with and without a rate limit in the snapshot, and the Manage accounts flow (the Accounts dialog opens, Close and the scrim close it and the focus returns).
- [e2e/sidebar.spec.ts](../../e2e/sidebar.spec.ts): 25 repositories with very long names at 1440 and 1100px (nothing scrolls sideways, the select, Select all, Clear, the VIA badges and the 25 gears are inside the sidebar, the last gear opens its dialog; a screenshot is attached), and the auto-collapse with a fake clock (a clicked button in the sidebar, a dialog open).
- [e2e/multipr.spec.ts](../../e2e/multipr.spec.ts): the sidebar is opened with the burger (`openSettings` in [e2e/helpers.ts](../../e2e/helpers.ts)), the rule is created and the activity read in the Status sync rules dialog it opens.
