# Installable app (PWA)

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 4.3. Related: [app-shell.md](app-shell.md), [security.md](security.md), [theme.md](theme.md).

## Purpose

The browser's "Install app" adds a dock or taskbar icon that opens the dashboard in its own
window. When the server is not running, the window shows a page that says to run
`scripts/start.sh`. The installed app uses the same persistent session cookie as the browser.

## Data model

None on the server. The web app manifest is `manifest.webmanifest` (name "PR Dashboards", short name "PR Dash", `start_url` and `scope` `/`, `display` `standalone`). `theme_color` is the `--tb-bg` token and `background_color` the `--page-bg` token of the light theme. Icons: 192 and 512 PNG, and a maskable 512 PNG.

## Code map

- [web/scripts/pwa-assets.mjs](../../web/scripts/pwa-assets.mjs): runs first in `pnpm -C web build`. Draws the icons as PNG with `node:zlib` and writes `web/public/manifest.webmanifest` and `web/public/icons/`, both git-ignored and left out of the `start.sh` source hash.
- [web/scripts/service-worker.mjs](../../web/scripts/service-worker.mjs): runs last. Writes `out/sw.js` from the export and the root `VERSION`.
- [web/src/app/offline/page.tsx](../../web/src/app/offline/page.tsx): the offline page, exported as `/offline/`.
- [web/src/lib/pwa.ts](../../web/src/lib/pwa.ts): `registerServiceWorker`, called from [providers.tsx](../../web/src/app/providers.tsx) in production builds. It is a normal module, so the CSP stays hash-only. [layout.tsx](../../web/src/app/layout.tsx) links the manifest and the icons.
- [server/internal/api/static.go](../../server/internal/api/static.go): serves `/sw.js` and `/manifest.webmanifest` without a session, with `Cache-Control: no-cache` and the types `text/javascript` and `application/manifest+json`.

## Key flows

1. Build: icons and manifest, `next build`, CSP hashes of the inline scripts, then `sw.js`.
2. Load: `registerServiceWorker` registers `/sw.js` with scope `/`. Install precaches the exported pages, `_next/static` files, icons and the manifest, then activates at once.
3. Activate deletes every `prdash-` cache that is not the current one. The cache is named `prdash-v<VERSION>`, so a release replaces the old files.
4. Fetch: navigations are network first and fall back to the cached page, then to `/offline/`. Precached static files are served from the cache. Everything else goes to the network untouched.

## Outside calls

None.

## Rules and invariants

- The worker never handles `/api/`, `/events`, `/oauth/` or `/auth/launch/`, and these pages are not precached. No API response is ever cached.
- The CSP has `worker-src 'self'` and `manifest-src 'self'`, no inline script or style is added. `pnpm -C web build` fails on an inline style.
- `sw.js` is never cached by the browser (`no-cache`), so an update is picked up on the next load.

## Tests

- [web/scripts/pwa.test.ts](../../web/scripts/pwa.test.ts): tokens, PNG output, manifest fields, precache list (static only, never the private paths), cache name and offline fallback in the generated worker.
- [web/src/lib/pwa.test.ts](../../web/src/lib/pwa.test.ts): registration, no support, failed registration.
- [server/internal/api/static_test.go](../../server/internal/api/static_test.go): content types, `no-cache` and the CSP of the worker and the manifest.
