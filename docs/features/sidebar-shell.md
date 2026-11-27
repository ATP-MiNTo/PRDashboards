# Sidebar shell

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [settings.md](settings.md), [command-table.md](command-table.md), [theme.md](theme.md), [app-shell.md](app-shell.md).

## Purpose

The left sidebar that holds the Settings content, and the accounts block pinned at its bottom. The burger that toggles it is in the left pill of the header ([header.md](header.md)). On a wide
screen (min-width 1024px) the sidebar is a column beside the page and is open by default; on a narrow
screen it is a drawer over the page, with a scrim, that opens from the burger. This is part of the v2 UI migration; the PRD and spec describe a Settings button in
the top bar, which no longer exists.

The sidebar is a glass pane 20rem (320px) wide with 20px padding, a 16px gap between its blocks, a radius of 0 20px 20px 0 and a hidden scrollbar. Its heading is a 36px gear square and "Settings" in Space Grotesk 18px bold, with the collapse chevron at the right. The Settings content follows ([settings.md](settings.md)), and the accounts block ends the pane.

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
4. Auto-collapse (wide, open): a 30 second `createIdleTimer` starts on mount and whenever the pointer is outside the sidebar. Entering the sidebar (mouse move) or focusing inside cancels it; leaving the sidebar or the window starts it again. When it fires and focus is inside the sidebar or `hold` is true (the Account panel is open), it starts over instead; otherwise the sidebar folds without saving.
5. Drawer: the `aside` is always mounted. `data-open` says whether it is open and the slide is a `transform` transition of .3s (`cubic-bezier(0, 0, .2, 1)`) on a narrow screen. The scrim fades in over .2s. While it is closed the `aside` is `inert` and `aria-hidden`, and its `visibility` turns hidden after the .3s, so nothing in it takes focus or is read. On the first render nothing animates (`data-animate` is off until the state has been read from the browser). Transitions are off with `prefers-reduced-motion: reduce`.
6. Accounts block: `SidebarAccounts` reads `GET /api/accounts` and shows an ACCOUNTS eyebrow, a GitHub row (login, and a badge: Token, OAuth or GitHub App, plus Reconnect when the token needs it) and a Jira row (site name, Atlassian, Reconnect or Not connected). "Manage accounts" (`aria-haspopup="dialog"`) opens the Connected accounts side panel ([auth.md](auth.md)), which replaces the Account button of the old top bar. Hold applies as before: the 30 second auto-collapse does not fold the sidebar while that panel is open. The "API usage" line of the design is not shown because the app has no API usage data.

## Outside calls

None from the shell. The accounts block reads `GET /api/accounts`, which reads the local database and makes no GitHub or Jira call. The sidebar content mounts when the sidebar first opens (page load on a wide screen) and stays mounted after a collapse (hidden with the `hidden` attribute). On mount it runs only local queries (settings, repos, rules, activity, retries); the Jira options request, the one that reaches Jira, waits until a rule form or the Exclude statuses list opens ([settings.md](settings.md)). Browser tabs therefore make no outside call by loading the dashboard.

## Rules and invariants

- Classes only: no `style` attribute and no `<style>` block, because the CSP is `style-src 'self'` ([security.md](security.md)). Dynamic state is a class or a data attribute.
- The blur is on a `::before` layer of the sidebar, not on the sidebar: `backdrop-filter` on an ancestor would become the containing block of the fixed popovers in the Dropdown and MultiSelect.
- The burger ([header.md](header.md)) and the collapse chevron are buttons with accessible names ("Settings sidebar", "Collapse sidebar") and `aria-expanded`; both have `aria-controls`.
- The sidebar column is 20rem wide, and the overlay is at most 20rem or 92vw. The table is sized to fit beside it at 1280px ([command-table.md](command-table.md)).
- The Account side panel keeps its own shell ([settings.md](settings.md)) and is not part of the sidebar; the sidebar only opens it.

## Tests

- [Shell.test.tsx](../../web/src/components/Shell/Shell.test.tsx): open and close from a burger and the chevron, saved and restored state, blocked storage, overlay (dialog, Escape, scrim, focus return, Tab wrap), auto-collapse with fake timers (pointer, focus, hold).
- [SettingsContent.test.tsx](../../web/src/components/Settings/SettingsContent.test.tsx) and [RulesSection.test.tsx](../../web/src/components/Settings/RulesSection.test.tsx): no Jira options request on mount, one when the rule form or the Exclude statuses list opens.
- [idleTimer.test.ts](../../web/src/components/Shell/idleTimer.test.ts): fire, cancel, restart, held.
- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx) and [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): the burger opens the sidebar, no Settings button, the accounts block with its badges and the Manage accounts flow.
- [e2e/multipr.spec.ts](../../e2e/multipr.spec.ts): the rule is created and the activity read in the sidebar, opened with the burger (`openSettings` in [e2e/helpers.ts](../../e2e/helpers.ts)).
