# Theme

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11.2. Related: [command-table.md](command-table.md), [storage.md](storage.md).

## Purpose

Light, Dark or Auto. Auto is light from 07:00 to 19:00 local time and dark otherwise. The choice is
stored on the server in the settings, so it follows the user across browsers.

## Data model

`Settings.theme` (`light`, `dark`, `auto`, default `auto`) in the `settings` table, through `GET/PUT /api/settings`. The resolved mode is the `data-mode` attribute of `<html>` (`light` or `dark`). `useTheme` also sets `data-theme` to the matching daisyUI theme: `winter` for light and `night` for dark.

## Code map

- [web/src/theme/theme.ts](../../web/src/theme/theme.ts): `autoMode`, `resolveMode`, `msUntilSwitch` and `daisyTheme` (the pure light/dark to `winter`/`night` mapping).
- [web/src/theme/useTheme.ts](../../web/src/theme/useTheme.ts): applies `data-mode` and `data-theme` and follows the clock while Auto. Returns `theme`, `mode` (the resolved mode, kept in step with the Auto timer) and `setTheme`.
- [web/tailwind.config.ts](../../web/tailwind.config.ts): the daisyUI plugin with the `winter` and `night` themes only. Its Tailwind and daisyUI setup is described in [app-shell.md](app-shell.md).
- [web/src/app/globals.css](../../web/src/app/globals.css): palette tokens for light and `[data-mode='dark']`, taken from the prototype, plus the names used by the older pages.
- [web/src/components/Settings/ThemeSwap.tsx](../../web/src/components/Settings/ThemeSwap.tsx): the rotating sun/moon toggle, a visually hidden checkbox named "Dark mode" (checked is dark) with two decorative SVGs, styled in `Settings.module.css`. The rotation is a CSS transition that `prefers-reduced-motion` turns off.
- [web/src/components/Settings/SettingsContent.tsx](../../web/src/components/Settings/SettingsContent.tsx): the Theme section (a sun/moon swap and an Auto checkbox), after Sessions and before Activity. The top bar has no theme control. [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx) keeps `useTheme` mounted, so the mode is applied while the Settings sidebar is closed.

## Key flows

1. The dashboard reads the settings, `useTheme` sets `data-mode` and `data-theme` on `<html>`.
2. In Auto it sets a timer for the next 07:00 or 19:00 and switches then.
3. Toggling the swap in the Settings sidebar is an explicit choice: it sends the full settings with `dark` or `light` to `PUT /api/settings` (this turns Auto off), then the query refetches and the mode follows. The swap shows the resolved `mode`, so in Auto it shows what the clock gives.
4. The "Auto (light from 07:00 to 19:00)" checkbox is checked while the theme is `auto`. Checking it saves `auto`. Unchecking it saves the mode in effect, so the look does not jump.
5. Both controls are disabled until the settings load.

## Outside calls

None.

## Rules and invariants

- Colours are CSS custom properties only. No inline styles.
- Before the settings load, the page uses the light tokens and has no `data-theme`, which no daisyUI component needs yet. The accounts and setup pages do not apply `data-mode` or `data-theme`.
- `data-mode` drives the palette in `globals.css`. `data-theme` only selects the daisyUI theme. Both are set in the same call, so they never disagree.
- The decision logic lives in pure functions with the clock passed in.

## Tests

- [web/src/theme/theme.test.ts](../../web/src/theme/theme.test.ts): Auto at each hour, explicit choices, time to the next switch, and the light/dark to `winter`/`night` mapping.
- [web/src/components/CommandTable/CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): applying the stored theme (both `data-mode` and `data-theme`), no theme control in the top bar. [SettingsContent.test.tsx](../../web/src/components/Settings/SettingsContent.test.tsx): the swap reflecting the theme, saving an explicit dark or light, turning Auto on and off, disabled while loading.
