# Theme

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11.2. Related: [command-table.md](command-table.md), [storage.md](storage.md).

## Purpose

Light, Dark or Auto. Auto is light from 07:00 to 19:00 local time and dark otherwise. The choice is
stored on the server in the settings, so it follows the user across browsers.

It also holds the look of the whole app, the approved glass design: fonts, colour and surface
tokens, shared control styles, cursors and the cross-fade between modes. Features style their own
CSS Modules from these tokens and never from fixed colours.

## Data model

`Settings.theme` (`light`, `dark`, `auto`, default `auto`) in the `settings` table, through `GET/PUT /api/settings`. The resolved mode is the `data-mode` attribute of `<html>` (`light` or `dark`). `useTheme` also sets `data-theme` to the matching daisyUI theme: `winter` for light and `night` for dark.

## Code map

- [web/src/theme/theme.ts](../../web/src/theme/theme.ts): `autoMode`, `resolveMode`, `msUntilSwitch` and `daisyTheme` (the pure light/dark to `winter`/`night` mapping).
- [web/src/theme/useTheme.ts](../../web/src/theme/useTheme.ts): applies `data-mode` and `data-theme` and follows the clock while Auto. Returns `theme`, `mode` (the resolved mode, kept in step with the Auto timer) and `setTheme`.
- [web/src/theme/transition.ts](../../web/src/theme/transition.ts): `applyMode` sets the two attributes, as a View Transition cross-fade when the mode really changes; `crossfade` is the guard around `document.startViewTransition`.
- [web/tailwind.config.ts](../../web/tailwind.config.ts): the daisyUI plugin with the `winter` and `night` themes only. Its Tailwind and daisyUI setup is described in [app-shell.md](app-shell.md).
- [web/src/app/globals.css](../../web/src/app/globals.css): the tokens for light and `[data-mode='dark']`, the page background, the `.glass` pane, the shared control classes (`ui-btn`, `ui-ghost`, `ui-select`, `ui-input`, `ui-controls`), the cursor rules, the press and transition rules for buttons and the View Transition timing. The names `--bg`, `--fg`, `--muted`, `--card`, `--ok` and `--danger` stay for the setup and accounts pages.
- [web/src/app/layout.tsx](../../web/src/app/layout.tsx): imports the two self-hosted font packages, [web/public/cursors/](../../web/public/cursors): the five cursor images.
- [web/src/components/Header/ThemeButton.tsx](../../web/src/components/Header/ThemeButton.tsx): the theme circle in the right header pill ([header.md](header.md)). The old sun/moon swap in Settings (`ThemeSwap.tsx`) is gone.
- The sidebar has no Theme block (it had a Theme heading and an "Auto (light from 07:00 to 19:00)" checkbox, removed because the header button does the job). [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx) keeps `useTheme` mounted and passes it to the header, so the mode is applied while the Settings sidebar is closed.

## Key flows

1. The dashboard reads the settings, `useTheme` sets `data-mode` and `data-theme` on `<html>`.
2. In Auto it sets a timer for the next 07:00 or 19:00 and switches then.
3. A change between light and dark cross-fades the whole page over 0.9s (`::view-transition-old(root)` and `-new(root)` in `globals.css`), through `document.startViewTransition` when the browser has it. Without it, or under `prefers-reduced-motion`, the attributes change at once. The first mode after load and an Auto timer that keeps the mode do not fade. A skipped transition (a newer one started) is ignored.
4. A click on the theme circle in the header is an explicit choice: it sends the full settings with `dark` or `light` to `PUT /api/settings` (this turns Auto off), then the query refetches and the mode follows. The icon is the mode the next click switches to (a sun in the dark, a moon in the light) and a half-filled circle in Auto.
5. A right-click, the context-menu key, Shift+click, or Shift+Enter or Shift+Space on the focused circle (a keyboard activation carries the Shift state) turns Auto on and shows the toast "Theme is Auto: light from 07:00 to 19:00" ([toast.md](toast.md)). The same gesture in Auto goes back to the mode that was chosen before, or the mode in effect if there was none, and says "Theme is light again" or "Theme is dark again".
6. The circle shows the state in its icon and its name ("Theme: light", "Theme: dark", "Theme: auto"), and its tooltip, also its accessible description, says how to reach Auto: "Click: light or dark. Auto: right-click, Shift+click, Shift+Enter or the Menu key". It is the only theme control, so Auto is reached from the keyboard through it.
7. The circle is disabled until the settings load.
8. The History view re-tints the page: see [history.md](history.md). It re-sets the daisyUI tokens and, in dark, the page and glass tokens under `:root[data-view='history']`, which is the one place the look depends on a view.

## Outside calls

None.

## Rules and invariants

The look (the approved glass design):

- Fonts: Inter (body, 16px on a 24px line) and Space Grotesk (`h1`, `h2` and `.display`, and the brand names). Both come from the `@fontsource-variable/inter` and `@fontsource-variable/space-grotesk` packages (exact versions in `web/package.json`), imported in `layout.tsx`, so Next emits the variable `woff2` files under `_next/static/media/`. The font-face rules have `unicode-range`, so a browser fetches only the scripts a page uses. The CSP already allows them (`font-src 'self'`) and the service worker precaches them with the rest of `_next/static/`. The server maps `.woff2` to `font/woff2`. Controls use 14px, small print 12px.
- Base colours come from the daisyUI themes: `--b1` surface, `--b2` page, `--bc` ink, `--p`, `--s`, `--a`, `--n`. `winter` is the default of `:root`, so they exist before `data-theme` is set, and `useTheme` sets `data-theme` with `data-mode`. Text is `oklch(var(--bc))` (light `oklch(0.4189 0.0539 255.8)`, dark `oklch(0.8415 0.008 265.8)`).
- Muted text: `--sub` (and `--eyebrow` for the small capitals) is ink at 85% in light and 80% in dark. The design mutes to 70% and 65%, which is 3.3 to 3.8:1 on the glass in light mode, so the steps are raised until every muted text reaches 4.5:1 on every surface, tinted rows and hovers included.
- Page: `html` carries `--page-image` over `--page-bg`, fixed. Light is three soft radial glows (`--p`, `--s`, `--a` at 28, 22 and 20%) over `oklch(var(--b2))`. Dark is the aurora, three radial glows over a `160deg` gradient from `#0d1226` through `#0a0f1f` to `#0c1424`.
- Glass: `.glass`, or `--glass-bg`, `--glass-border`, `--glass-blur`, `--glass-shadow` and `--radius-glass` (1.1rem) in a CSS Module. Light is `oklch(var(--b1) / .55)`, a `1px` `oklch(var(--bc) / .12)` border and `blur(16px) saturate(140%)`. Dark is a white-to-clear gradient, a `rgba(255,255,255,.11)` border, `blur(22px) saturate(150%)`, an inset highlight and a `0 10px 30px -12px` shadow. Do not put `backdrop-filter` on an ancestor of a `position: fixed` popover; the sidebar blurs a `::before` layer for this reason. Popovers, the FAB and pickers are opaque (`--surface`).
- Radius scale: `--radius-ctl` .5rem (controls), `--radius-stack` .6rem, `--radius-box` 1rem (popovers), `--radius-glass` 1.1rem, `--radius-pill`.
- Roles and stacks: `--rc-a` cyan-teal for assigned and opened by the user (`#bfeaf1`, dark `#1d5560`), `--rc-r` red for review requested (`#f9c9c9`, dark `#6a363c`) and `--stack` (`#8b6cf0`, dark `#a994ff`) are the colours of the design. The legend dots use them as they are. `--card-a-*`, `--card-r-*` and `--card-b-*` (background and line) tint a whole card: 28% of the colour over the surface in light, 46/54 split for both, the translucent teal and pink gradients of the design in dark. `--stack-group-bg`, `--stack-group-line` (dark only), `--stack-group-shadow`, `--stack-chip-fg` and `--stack-step-bg` draw a stack group ([stacked-prs.md](stacked-prs.md)). `--diff-add` and `--diff-del` colour the size of a change. `--blocked-bg`, `--blocked-bg-hover`, `--blocked-bg-on` and `--blocked-fg` colour the Blocked stat tile and its compact button: a pastel red wash (alpha .22, .28 and .38 of `rgb(235 90 100)` in light; .18, .23 and .32 of `rgb(240 110 130)` in dark) and the number in `#8a1420` (light) or `#ffc9cc` (dark), at least 4.5:1 on every state.
- Focus: `:focus-visible` is a 2px solid ring in the ink colour, 2px away, as daisyUI draws it.
- Controls: `ui-btn` is daisyUI `btn btn-sm btn-outline` (32px, radius .5rem, 1px ink border, hover fills with ink and turns the text to the surface colour, disabled is 20% neutral with 20% ink text). Dark uses the translucent fill and the white 18% border of the design, and keeps the same ink fill on hover. `ui-ghost` is `btn-ghost` (hover fills with 20% ink), `ui-select` is `select select-sm` and `ui-input` an input with a border. A checkbox is 1rem and takes the ink colour when checked; `ui-check` is the daisyUI `checkbox-xs` (round corners, a check mark that slides in over .2s). `--btn-on-bg` and `--btn-on-border` are the current page button of the pager (the third surface a tenth darker; in dark the translucent fill with the 18% border), and `--btn-off-line` is the border of a disabled button (none in light, white 8% in dark). A select shows a soft 20% ink ring while it has focus. A CSS Module takes one with `composes: ui-btn from global;`; a container takes it for every button, select and text field inside with `composes: ui-controls from global;` (the Settings sidebar), and then needs two-class selectors for a borderless button. The Merge button is `ui-btn` in the blue `--merge` (`#007fff`, hover `--merge-hover` `#1a8cff`, white text, radius .5rem, 32px, the merge mark) with an aura that pulses for 2.6s while enabled (`aura` keyframes in `Controls.module.css`, the same look for the button in the merge dialog). The primary action is `--accent` (`oklch(var(--p))`).
- Every `button` has the 0.2s transition on colour, background, border, opacity, shadow and transform, and scales to 0.95 while pressed. Reduced motion turns the transition, the press and the aura off.
- Cursors: `/cursors/arrow.svg` (hotspot 5 3) on the page, `hand.svg` (12 4) on buttons, links, tabs, summaries, selects, checkboxes, radios and ranges, `merge.svg` (16 16) on Merge buttons, `no.svg` (16 16) on disabled buttons, selects and inputs, and `ready.svg` (16 16) on the Ready button of a draft. Text fields keep the text cursor. The files are static (`web/public/cursors`, 32x32 SVG), referenced as `cursor: url(/cursors/x.svg) x y, fallback`, so the CSP needs no change: a cursor image is governed by `img-src 'self'`. The service worker precaches them and the server serves `.svg` as `image/svg+xml`.

Rules:

- Colours are CSS custom properties only. No inline styles, no external fonts or images.
- Text on any surface has at least 4.5:1 contrast, except the Merge button (white on `#007fff`, 3.8:1, and 3.4:1 on hover), whose colours are fixed by the design, and disabled controls.
- Before the settings load, the page uses the light tokens and has no `data-theme`, which no daisyUI component needs yet. The accounts and setup pages do not apply `data-mode` or `data-theme`.
- `data-mode` drives the palette in `globals.css`. `data-theme` only selects the daisyUI theme. Both are set in the same call, so they never disagree.
- The decision logic lives in pure functions with the clock passed in.

## Tests

- [web/src/theme/theme.test.ts](../../web/src/theme/theme.test.ts): Auto at each hour, explicit choices, time to the next switch, and the light/dark to `winter`/`night` mapping.
- [web/src/theme/transition.test.ts](../../web/src/theme/transition.test.ts): the cross-fade runs the change inside a view transition, is skipped under reduced motion or without browser support, ignores a skipped transition, and `applyMode` fades only a real change.
- [server/internal/api/static_test.go](../../server/internal/api/static_test.go): fonts are served as `font/woff2` and cursors as `image/svg+xml`, with the CSP `font-src 'self'` and `img-src 'self'`. [web/scripts/pwa.test.ts](../../web/scripts/pwa.test.ts): fonts and cursors are precached.
- [web/src/components/CommandTable/CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): applying the stored theme (both `data-mode` and `data-theme`), the theme circle in the page. [Header.test.tsx](../../web/src/components/Header/Header.test.tsx): the circle's state names, light and dark by click, Auto by right-click and Shift+click with the toast, the tooltip and description, back to the earlier mode, disabled while loading. [SettingsContent.test.tsx](../../web/src/components/Settings/SettingsContent.test.tsx): the sidebar has no Theme heading and no Auto checkbox.
