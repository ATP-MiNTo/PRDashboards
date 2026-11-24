# Theme

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11.2. Related: [command-table.md](command-table.md), [storage.md](storage.md).

## Purpose

Light, Dark or Auto. Auto is light from 07:00 to 19:00 local time and dark otherwise. The choice is
stored on the server in the settings, so it follows the user across browsers.

## Data model

`Settings.theme` (`light`, `dark`, `auto`, default `auto`) in the `settings` table, through `GET/PUT /api/settings`. The resolved mode is the `data-mode` attribute of `<html>`.

## Code map

- [web/src/theme/theme.ts](../../web/src/theme/theme.ts): `autoMode`, `resolveMode`, `msUntilSwitch`.
- [web/src/theme/useTheme.ts](../../web/src/theme/useTheme.ts): applies the mode and follows the clock while Auto.
- [web/src/app/globals.css](../../web/src/app/globals.css): palette tokens for light and `[data-mode='dark']`, taken from the prototype, plus the names used by the older pages.
- [web/src/components/CommandTable/TopBar.tsx](../../web/src/components/CommandTable/TopBar.tsx): the Light, Dark and Auto buttons.

## Key flows

1. The dashboard reads the settings, `useTheme` sets `data-mode` on `<html>`.
2. In Auto it sets a timer for the next 07:00 or 19:00 and switches then.
3. Clicking a button sends the full settings with the new theme to `PUT /api/settings`, then the query refetches and the mode follows.

## Outside calls

None.

## Rules and invariants

- Colours are CSS custom properties only. No inline styles.
- Before the settings load, the page uses the light tokens. The accounts and setup pages do not apply `data-mode`.
- The decision logic lives in pure functions with the clock passed in.

## Tests

- [web/src/theme/theme.test.ts](../../web/src/theme/theme.test.ts): Auto at each hour, explicit choices, time to the next switch.
- [web/src/components/CommandTable/CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): applying and saving the theme.
