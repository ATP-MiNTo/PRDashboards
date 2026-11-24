# App shell, launch and session

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 3, 4 and 8.3. Related: [security.md](security.md).

## Purpose

Starts the single binary, serves the embedded UI and the API on `127.0.0.1`, and signs the browser
in with a one-time launch URL. This is milestone 1 (skeleton): the UI is a placeholder page that
shows "Signed in" and the server version. The dashboard, storage, OAuth and the poller come in later
milestones.

Differences from the spec in milestone 1:

- No SSE route and no web app manifest or service worker. The PWA part of TECH_SPEC 4.3 is
  milestone 9.
- The Next export has pages `/` and `/auth/launch/` only. `/setup` and `/auth/done` come with OAuth.
- TanStack Query is not used yet. The placeholder page calls the typed client directly.

## Data model

- Files in the data dir (default `~/.prdashboards`, mode 0700): `prdash.lock` (exclusive lock),
  `launch.token` (newest launch token, mode 0600), `session.key` (cookie signing key, mode 0600).
- API schemas (from [api/openapi.yaml](../../api/openapi.yaml)): `CreateSessionRequest`
  `{launchToken}`, `Session` `{csrfToken, expiresAt, version}`, `Problem`.
- Flags: `--port` (default 4317), `--data-dir`, `--no-browser`.
- `VERSION` is stamped into the binary with `-ldflags "-X main.version=..."` and returned as
  `Session.version`.

## Code map

Server:

- [server/cmd/prdash/main.go](../../server/cmd/prdash/main.go): flags, signal handling, exit codes.
- [server/cmd/prdash/run.go](../../server/cmd/prdash/run.go): `run`, the start order, listener, shutdown, and `openRunning` for a second start.
- [server/cmd/prdash/datadir.go](../../server/cmd/prdash/datadir.go): `expandHome` and `prepareDataDir`.
- [server/cmd/prdash/browser.go](../../server/cmd/prdash/browser.go): `browserCommand` and `openBrowser` (no shell).
- [server/web.go](../../server/web.go): `go:embed` of `server/web/`, filled by the web build. Only `.gitkeep` is committed.
- [server/internal/api/static.go](../../server/internal/api/static.go): static UI server and the "UI not built" page.
- [server/internal/api/server.go](../../server/internal/api/server.go): `New`, handlers.
- [go.work](../../go.work): lets `go test ./server/...` run from the repo root.

Web:

- [web/src/app/page.tsx](../../web/src/app/page.tsx): `/` placeholder, calls `GET /api/session`.
- [web/src/app/auth/launch/page.tsx](../../web/src/app/auth/launch/page.tsx): reads the fragment, posts the token, goes to `/`.
- [web/src/lib/launch.ts](../../web/src/lib/launch.ts): `parseLaunchToken` and `runLaunch` (pure logic, no DOM).
- [web/src/api/client.ts](../../web/src/api/client.ts): typed fetch client over the generated [schema.d.ts](../../web/src/api/schema.d.ts).
- [web/next.config.ts](../../web/next.config.ts): `output: 'export'`, `trailingSlash: true`.

Scripts and CI:

- [scripts/start.sh](../../scripts/start.sh), [start.command](../../scripts/start.command), [start.cmd](../../scripts/start.cmd): check tools, rebuild when the source hash changed, run `bin/prdash`. Extra arguments go to the binary.
- [scripts/check-docs.mjs](../../scripts/check-docs.mjs), [scripts/check-version.mjs](../../scripts/check-version.mjs), [scripts/check-coverage.sh](../../scripts/check-coverage.sh).
- [.github/workflows/ci.yml](../../.github/workflows/ci.yml): jobs `go`, `web`, `contract`, `docs`.

## Key flows

1. `scripts/start.sh`: check `go` (against `server/go.mod`), `node` (against `.nvmrc`) and `pnpm`.
   Hash `web/`, `server/` (without `server/web/`), `api/` and the lockfiles. When the hash differs
   from `.build-hash` or `bin/prdash` is missing: `pnpm install --frozen-lockfile`,
   `pnpm -C web build` (which also writes `out/csp.json`), copy `web/out/*` into `server/web/`,
   `go build -trimpath`, store the hash. Then `exec bin/prdash "$@"`.
2. `run`: expand the data dir, `prepareDataDir` (create 0700, refuse a group- or world-writable
   dir on POSIX), take the lock with `flock`.
3. Lock held by another instance: `openRunning` reads `launch.token` and opens
   `http://127.0.0.1:<port>/auth/launch#t=<token>` (or prints it with `--no-browser`), exit 0.
4. Lock acquired: bind `tcp4 127.0.0.1:<port>` (a busy port exits with a message), load or create
   `session.key`, create the `Launcher` (writes `launch.token`), build the handler with `api.New`,
   serve, and open the browser or print the URL to stderr. Rotation runs every 60 s.
5. Browser: `/auth/launch/#t=...` loads the static page, which clears the fragment with
   `history.replaceState`, posts `{launchToken}` to `POST /api/session`, and on `204` calls
   `location.replace('/')`. `/` calls `GET /api/session` and shows "Signed in" and the version,
   or "Open the app with scripts/start.sh" on `401`.
6. SIGINT or SIGTERM: `srv.Shutdown` with a 10 s limit, stop rotation, remove `launch.token`,
   release the lock.

## Outside calls

None.

## Rules and invariants

- The listener is `tcp4` on `127.0.0.1` and the port is fixed. Never `0.0.0.0`, `::` or a random port.
  When the port is not 4317 the binary logs a warning, because OAuth callbacks are registered for 4317.
- Order at start: data dir, mode check, lock, bind. The lock is released and `launch.token` removed
  on shutdown.
- The browser opens through `open`, `xdg-open` or `rundll32 url.dll,FileProtocolHandler`, run
  without a shell.
- The launch URL is never written to the log.
- The UI is served with `http.FileServerFS` semantics: `/auth/launch` redirects to `/auth/launch/`,
  directories without an `index.html` are not listed, dotfiles and `csp.json` are not served, and an
  unknown path gets `404.html` from the export when present. With no web build embedded, every
  non-API path answers `503` with a plain page that says to run `scripts/start.sh`.
- The web app has no inline script beyond what Next emits, no external fonts or CDNs, and no
  `dangerouslySetInnerHTML` (ESLint enforces it).
- Generated code (`server/internal/api/gen`, `web/src/api/schema.d.ts`) is committed and never
  edited by hand. `pnpm gen` regenerates it, and CI fails when the result differs.
- Every merged change bumps `VERSION` and `CHANGELOG.md` (checked by `scripts/check-version.mjs`).

## Tests

- [server/cmd/prdash/main_test.go](../../server/cmd/prdash/main_test.go): flags, data dir modes, browser commands, start, shutdown cleanup, second instance with an injected opener, busy port, bind address.
- [server/internal/api/static_test.go](../../server/internal/api/static_test.go): static serving, redirect, 404 page, empty embed fallback.
- [server/internal/api/api_test.go](../../server/internal/api/api_test.go): launch flow end to end.
- [web/src/lib/launch.test.ts](../../web/src/lib/launch.test.ts), [web/src/app/page.test.tsx](../../web/src/app/page.test.tsx), [web/src/app/auth/launch/page.test.tsx](../../web/src/app/auth/launch/page.test.tsx), [web/src/api/client.test.ts](../../web/src/api/client.test.ts).
- [scripts/check-docs.test.mjs](../../scripts/check-docs.test.mjs) and [scripts/check-version.test.mjs](../../scripts/check-version.test.mjs) (`pnpm test:scripts`).
