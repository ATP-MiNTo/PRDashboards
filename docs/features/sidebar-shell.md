# Sidebar shell and floating burger

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [settings.md](settings.md), [command-table.md](command-table.md), [theme.md](theme.md), [app-shell.md](app-shell.md).

## Purpose

The left sidebar that holds the Settings content, and the floating burger that toggles it. On a wide
screen (min-width 1024px) the sidebar is a column beside the page and is open by default; on a narrow
screen it is a modal overlay that opens from the burger. The burger stays at the top left of the page
while it scrolls. This is part of the v2 UI migration; the PRD and spec describe a Settings button in
the top bar, which no longer exists.

## Data model

No server data. Browser storage: `localStorage` key `prd-side` (`1` open, `0` folded), written only
when the user toggles the sidebar on a wide screen. Every read and write is in `try/catch`; when
storage is blocked the choice lasts for the visit. The overlay state on a narrow screen and the
auto-collapse are not saved. Scroll position and the wide-screen media query are read from the window.

## Code map

- [web/src/components/Shell/useSidebar.ts](../../web/src/components/Shell/useSidebar.ts): `useSidebar({ hold })` (wide or narrow, open state, `toggle`, `close`, the `aside` ref, auto-collapse) and `useScrolled(threshold = 24)`. Constants `SIDEBAR_KEY`, `WIDE_QUERY`, `AUTO_COLLAPSE_MS`.
- [web/src/components/Shell/idleTimer.ts](../../web/src/components/Shell/idleTimer.ts): `createIdleTimer`, the pure one-shot timer with `arm`, `cancel`, `isArmed` and an `isHeld` check.
- [web/src/components/Shell/Sidebar.tsx](../../web/src/components/Shell/Sidebar.tsx): the frosted `aside` with the "Settings" title and the collapse chevron, as a column or as an overlay with a scrim.
- [web/src/components/Shell/Burger.tsx](../../web/src/components/Shell/Burger.tsx): the sticky burger pill. [BrandIcon.tsx](../../web/src/components/Shell/BrandIcon.tsx): the decorative repo and branch icon, also used by the top bar.
- [web/src/components/Shell/Shell.module.css](../../web/src/components/Shell/Shell.module.css): glass look from theme tokens, pill and title transitions, animations.
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): wires the hook, the burger, the sidebar with `SettingsContent` ([settings.md](settings.md)) and the top bar.

## Key flows

1. Mount: `useSidebar` reads `prd-side` and the `(min-width: 1024px)` media query (through `useSyncExternalStore`, so the first render matches the static export: narrow and closed). On a wide screen the sidebar is open unless `prd-side` is `0`.
2. The burger and the chevron call `toggle` and `close`. On a wide screen this saves `prd-side`. On a narrow screen it only changes the overlay.
3. Overlay: the `aside` is `role="dialog"` with `aria-modal`, focus moves into it, Tab wraps inside, Escape or a click on the scrim closes it, and focus returns to the burger. On a wide screen the `aside` is a plain labelled region and takes no focus.
4. Auto-collapse (wide, open): a 30 second `createIdleTimer` starts on mount and whenever the pointer is outside the sidebar. Entering the sidebar (mouse move) or focusing inside cancels it; leaving the sidebar or the window starts it again. When it fires and focus is inside the sidebar or `hold` is true (the Account panel is open), it starts over instead; otherwise the sidebar folds without saving.
5. Burger pill: `useScrolled` turns on after `window.scrollY` passes 24px. The pill then gets a frosted background and its title ("PR Dashboards" and the icon, `aria-hidden`) grows from zero width and fades in; the top bar's brand fades out so the name is not shown twice. Both transitions are off with `prefers-reduced-motion: reduce`.

## Outside calls

None. The Settings content makes its own calls when it first mounts ([settings.md](settings.md)); the sidebar content mounts when the sidebar first opens and stays mounted after a collapse (hidden with the `hidden` attribute).

## Rules and invariants

- Classes only: no `style` attribute and no `<style>` block, because the CSP is `style-src 'self'` ([security.md](security.md)). Dynamic state is a class or a data attribute.
- The blur is on a `::before` layer of the sidebar, not on the sidebar: `backdrop-filter` on an ancestor would become the containing block of the fixed popovers in the Dropdown and MultiSelect.
- The burger and the collapse chevron are buttons with accessible names ("Settings sidebar", "Collapse sidebar") and `aria-expanded`; the burger has `aria-controls`.
- The burger wrapper has height 0, so it adds no scroll space. The page adds no bottom padding for the shell.
- The Account side panel keeps its own shell ([settings.md](settings.md)) and is not part of the sidebar.

## Tests

- [Shell.test.tsx](../../web/src/components/Shell/Shell.test.tsx): open and close from the burger and the chevron, saved and restored state, blocked storage, overlay (dialog, Escape, scrim, focus return, Tab wrap), auto-collapse with fake timers (pointer, focus, hold), the pill's stuck state after a scroll.
- [idleTimer.test.ts](../../web/src/components/Shell/idleTimer.test.ts): fire, cancel, restart, held.
- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx) and [TopBar.test.tsx](../../web/src/components/CommandTable/TopBar.test.tsx): the burger opens the sidebar, no Settings button, name followed by the icon.
- [e2e/multipr.spec.ts](../../e2e/multipr.spec.ts): the rule is created and the activity read in the sidebar, opened with the burger (`openSettings` in [e2e/helpers.ts](../../e2e/helpers.ts)).
