# Header

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [sidebar-shell.md](sidebar-shell.md), [search.md](search.md), [toast.md](toast.md), [theme.md](theme.md), [command-table.md](command-table.md), [poller.md](poller.md).

## Purpose

The top of the dashboard: two floating glass pills that stay at the top while the page scrolls, over a title bar that scrolls away. It replaces the full-width top bar of the earlier UI, which had the brand, the source status, the Sync button and the Account button. Sync and Account no longer sit in the header: Refresh is a circle in the right pill and Account is in the sidebar ([sidebar-shell.md](sidebar-shell.md)). The PRD and spec describe a plain top bar; this is part of the v2 UI migration.

- Left pill: the burger ("Settings sidebar") and the brand "PRDashboards" (Space Grotesk, 18px, 600) followed by the repo and branch icon. The name slides in after the page scrolls (max width 14rem, .4s).
- Right pill: the compact summary (only once the stat strip has scrolled out of view, at every width; its Blocked button has the same red number as the Blocked tile, [command-table.md](command-table.md)), the search field (from 768px, [search.md](search.md)), the Refresh circle, the history circle and the theme circle.
- Both pills are 44px high, 22px from the top (`--pill-h` and `--pill-top` in globals.css; with `--header-bottom` they place the toasts under the pills, [toast.md](toast.md)), 12px from the sides, and transparent until the page scrolls past 24px, then glass (`oklch(var(--b1) / .94)`, blur 10px, a 15% ink border, a soft shadow).
- Under the pills a glass title bar holds the `h1` (the same name and icon). It scrolls away with the page.

## Data model

No server data. The search text is `useListPrefs` state ([search.md](search.md)). The theme is `Settings.theme` through `GET/PUT /api/settings` ([theme.md](theme.md)). No browser storage.

## Code map

- [web/src/components/Header/Header.tsx](../../web/src/components/Header/Header.tsx): the sticky bar, the two pills and the title bar. Props carry the sidebar state, the search text, the snapshot, the sync status and the theme. Optional props: `history` (`{active, onToggle}`, adds the history circle), `title` (the title bar text, "PRDashboards · History" while the History view is open), `compactStats` and `showCompactStats` (the summary slot of the right pill; `CommandTable` fills it with `CompactStats`).
- [RefreshButton.tsx](../../web/src/components/Header/RefreshButton.tsx): the Refresh circle, its spin and its popover.
- [SyncDetails.tsx](../../web/src/components/Header/SyncDetails.tsx): the content of the popover: source status of GitHub and Jira (`sourceLabel`, with the time left of a rate limit), the sync times and the countdown. Moved from the old `CommandTable/TopBar.tsx`.
- [ThemeButton.tsx](../../web/src/components/Header/ThemeButton.tsx): the theme circle and `AUTO_TOAST`.
- [HistoryButton.tsx](../../web/src/components/Header/HistoryButton.tsx): the history circle.
- [SearchBox.tsx](../../web/src/components/Header/SearchBox.tsx), [Icon.tsx](../../web/src/components/Header/Icon.tsx) (a 16px stroke icon wrapper) and [Header.module.css](../../web/src/components/Header/Header.module.css).
- [web/src/lib/refresh.ts](../../web/src/lib/refresh.ts): `changedCount` and `refreshToast`, the pure text of the toast after a refresh.
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): `useTheme`, `useSidebar` and `useScrolled` feed the header; the page-wide `ToastProvider` wraps it.

## Key flows

1. Scroll: `useScrolled` turns `stuck` on after 24px. The pills get the glass background and the name in the left pill grows from zero width and fades in; the title bar is not touched. Both transitions are off under reduced motion.
2. Refresh: a click calls `POST /api/sync` (`useSyncNow`), the same call the Sync button made. The icon spins while the status says running (a WAAPI animation that finishes its turn before it stops), the button has `aria-busy` and ignores clicks meanwhile. When the sync ends, `refreshToast` compares the snapshot from before with the one after and shows one toast: "Refreshed. N pull requests changed." (success), "Refreshed. Nothing changed since 14:05." (info), or "Refreshed. GitHub rate limited." and the same for Jira (error). The toast waits at most `TOAST_WAIT_MS` for the new snapshot and is shown only for a refresh this tab asked for, never for one the poller ran.
3. Sync details: hovering the Refresh circle or focusing it opens a popover with `SyncDetails` (`role="tooltip"`, linked by `aria-describedby`); leaving or blurring closes it. The status text is a `role="status"` element named "Source status". Blocking warnings (blocked organizations, rate limit and error banners, the disconnected notice) stay in `OrgBanner` under the header.
4. Theme: a click switches between light and dark and saves it (this leaves Auto). A right-click, the context-menu key or Shift+click turns Auto on and shows "Theme is Auto: light from 07:00 to 19:00"; the same gesture in Auto goes back to the mode chosen before and says "Theme is dark again" or "Theme is light again". The icon is the mode a click would switch to (a sun in the dark, a moon in the light, a half-filled circle in Auto) and changes with a short spin. The button is disabled until the settings load. This button is the only theme control: the sidebar has no Theme block. Auto is reachable without a pointer too: Shift+Enter or Shift+Space on the focused button (a keyboard activation carries the Shift state, like Shift+click) or the context-menu key. Its tooltip, which is also its accessible description, says so: "Click: light or dark. Auto: right-click, Shift+click, Shift+Enter or the Menu key" (`THEME_TITLE`).
5. Compact summary: `CommandTable` watches the stat strip with an `IntersectionObserver` (no scroll listener; the top 66px under the pills do not count as viewport). While the strip is in view, or in the History view (which has no strip), the summary is hidden: zero width, transparent, `aria-hidden` and `inert`, so it takes no focus and is not read. Once the strip has scrolled out above the pills it opens in the right pill: Showing, Ready, CI, Blocked, Draft, Stale and Stacks, each a button with the same filter as its tile (`aria-pressed`, the name says the number: "Blocked: 2"), a small label and a bold number, the Ready number in the accent colour. It opens with `max-width` .4s, `opacity` .3s and `margin` .4s (the prototype's), none of it under `prefers-reduced-motion`. The pills float over the page in a bar that is no higher than nothing, so nothing moves in the page when it opens. It shrinks, and scrolls without a scrollbar, before anything else in the pill, and below 1440px the name leaves the left pill while it shows, to give it room (the sidebar takes 320px of the page).
6. History: the history circle is a toggle (`aria-pressed`). Its clock hand winds three turns one way when the History view opens and the other way when it closes (not on the first render). `CommandTable` swaps the list for the History view with the 0.9s page cross-fade ([history.md](history.md)); the title bar then reads "PRDashboards · History" and the search filters the history.

## Outside calls

Only the one the person asks for: Refresh posts `POST /api/sync`, which makes the server refresh from GitHub and Jira ([poller.md](poller.md)). Nothing in the header calls the server on mount or on a timer, and the popover reads the status already in the cache.

## Rules and invariants

- Every control has an accessible name: "Settings sidebar" (with `aria-expanded` and `aria-controls`), "Search pull requests", "Refresh", "Theme: light|dark|auto", "History". The name text in the left pill and the title is decorative where the `h1` has it.
- Classes and data attributes only: no `style` attribute, no `<style>` block, no `dangerouslySetInnerHTML`.
- The pills are `position: sticky`, so they take no extra scroll space and stay above the table.
- Controls are 32px circles with the `ui-ghost` hover and the focus ring from [theme.md](theme.md); the popover is opaque (`--surface`).
- Animations (the spin, the settle, the clock hand, the icon swap, the name sliding in) stop under `prefers-reduced-motion: reduce`.

## Tests

- [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): brand name followed by its icon, the burger, the stuck state, search, Refresh (busy, popover on hover and focus, source status, rate limit countdown), the refresh toasts, the theme button (click, Auto by right-click and Shift+click, the tooltip and description, toasts, disabled while loading) and the history toggle. [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx) swaps the list for the History view and back.
- [refresh.test.ts](../../web/src/lib/refresh.test.ts): `changedCount` and the toast text for each case.
- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): Refresh and the popover in the page, no Sync or Account button, the theme button, the burger opening the sidebar, the compact summary and its filters.
- [e2e/ratelimit.spec.ts](../../e2e/ratelimit.spec.ts) and the other specs: Refresh replaces the Sync button.
