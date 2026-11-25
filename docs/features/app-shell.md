# App shell, launch and session

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 3, 4 and 8.3. Related: [security.md](security.md), [storage.md](storage.md), [auth.md](auth.md).

## Purpose

Starts the single binary, serves the embedded UI and the API on `127.0.0.1`, and signs the browser
in with a one-time launch URL. `/` is the dashboard ([command-table.md](command-table.md)) and
`/accounts/` shows "Signed in", the server version and the Connected accounts panel
([auth.md](auth.md)); the dashboard shows the same panel in its Account side panel. The poller runs inside the binary ([poller.md](poller.md)).

Differences from the spec:

- `GET /events` streams server-sent events ([poller.md](poller.md)). The web app manifest and
  service worker are described in [installable-app.md](installable-app.md).
- The dashboard has a shared side panel ([settings.md](settings.md)) used by the Account and Settings buttons of the top bar.
- The Next export has pages `/`, `/accounts/`, `/auth/launch/`, `/setup/`, `/auth/done/` and `/oauth/finish/`.
- TanStack Query 5 carries all server state in the UI (`web/src/api/queries.ts`).

## Data model

- Files in the data dir (default `~/.prdashboards`, mode 0700): `prdash.lock` (exclusive lock),
  `launch.token` (JSON `{"port":N,"token":"..."}`, newest launch token, mode 0600, atomic write),
  `data.db` (SQLite) and the secrets file: `vault.enc` with a keychain, `secrets.enc` without one
  (all 0600, see [storage.md](storage.md)). The cookie signing key is the secrets item `session.key`. A
  `session.key` file from an earlier version is imported once and deleted.
- API schemas (from [api/openapi.yaml](../../api/openapi.yaml)): `CreateSessionRequest`
  `{launchToken}`, `Session` `{csrfToken, expiresAt, version}`, `Problem`.
- Flags: `--port` (default 4317), `--data-dir`, `--no-browser`, and `--fake` in the e2e build only.
- `VERSION` is stamped into the binary with `-ldflags "-X main.version=..."` and returned as
  `Session.version`.

## Code map

Server:

- [server/cmd/prdash/main.go](../../server/cmd/prdash/main.go): flags, signal handling, exit codes.
- [server/cmd/prdash/run.go](../../server/cmd/prdash/run.go): `run`, the start order and wiring, `newHTTPServer` (timeouts), shutdown, and `openRunning` for a second start.
- [server/cmd/prdash/datadir.go](../../server/cmd/prdash/datadir.go): `expandHome`, `prepareDataDir` and `checkPrivateFiles`.
- [server/cmd/prdash/fake_e2e.go](../../server/cmd/prdash/fake_e2e.go): `--fake` (e2e build tag only).
- [server/cmd/prdash/browser.go](../../server/cmd/prdash/browser.go): `browserCommand` and `openBrowser` (no shell).
- [server/web.go](../../server/web.go): `go:embed` of `server/web/`, filled by the web build. Only `.gitkeep` is committed.
- [server/internal/api/static.go](../../server/internal/api/static.go): static UI server and the "UI not built" page.
- [server/internal/api/server.go](../../server/internal/api/server.go): `New`, handlers.
- [go.work](../../go.work): lets `go test ./server/...` run from the repo root.

Web:

- [web/src/app/page.tsx](../../web/src/app/page.tsx): `/`, calls `GET /api/session` and shows the Command Table.
- [web/src/app/accounts/page.tsx](../../web/src/app/accounts/page.tsx): `/accounts/`, the signed-in state and the accounts panel.
- Other pages: `/setup/`, `/auth/done/` and `/oauth/finish/` are described in [auth.md](auth.md).
- [web/src/app/auth/launch/page.tsx](../../web/src/app/auth/launch/page.tsx): reads the fragment, posts the token, goes to `/`.
- [web/src/lib/launch.ts](../../web/src/lib/launch.ts): `parseLaunchToken` and `runLaunch` (pure logic, no DOM).
- [web/src/api/client.ts](../../web/src/api/client.ts): typed fetch client over the generated [schema.d.ts](../../web/src/api/schema.d.ts).
- [web/next.config.ts](../../web/next.config.ts): `output: 'export'`, `trailingSlash: true`.

Scripts and CI:

- [scripts/start.sh](../../scripts/start.sh), [start.command](../../scripts/start.command), [start.cmd](../../scripts/start.cmd): check tools, rebuild when the source hash changed, run `bin/prdash`. Extra arguments go to the binary.
- [scripts/e2e.mjs](../../scripts/e2e.mjs): `pnpm e2e`, builds the web export and the e2e binary, runs Playwright.
- [scripts/check-docs.mjs](../../scripts/check-docs.mjs), [scripts/check-version.mjs](../../scripts/check-version.mjs), [scripts/check-coverage.sh](../../scripts/check-coverage.sh).
- [.github/workflows/ci.yml](../../.github/workflows/ci.yml): jobs `go`, `web`, `e2e`, `contract`, `docs`.

## Key flows

1. `scripts/start.sh`: check `go` (against `server/go.mod`), `node` (against `.nvmrc`) and `pnpm`.
   Hash `web/`, `server/` (without `server/web/`), `api/` and the lockfiles. When the hash differs
   from `.build-hash` or `bin/prdash` is missing: `pnpm install --frozen-lockfile`,
   `pnpm -C web build` (which also writes `out/csp.json`), copy `web/out/*` into `server/web/`,
   `go build -trimpath`, store the hash. Then `exec bin/prdash "$@"`.
2. `run`: expand the data dir, `prepareDataDir` (create 0700, refuse a group- or world-writable
   dir on POSIX), take the lock with `flock`, then `checkPrivateFiles` (refuse a `session.key`,
   `secrets.enc` or `data.db` that group or others can access, naming the `chmod 600` to run).
3. Lock held by another instance: `openRunning` reads the port and token from `launch.token` and
   opens `http://127.0.0.1:<port>/auth/launch#t=<token>` (or prints it with `--no-browser`), exit
   0. The port comes from the file, so a second start without `--port` still finds an instance on
   another port.
4. Lock acquired: bind `tcp4 127.0.0.1:<port>` (a busy port exits with a message), open the
   secrets store (keychain-keyed vault, else the passphrase prompt, else exit when there is no terminal), open
   SQLite, load or create the session key, load sessions, create the `Launcher` (writes
   `launch.token` with the port), build the `netguard` client, the GitHub client and `auth.OAuth`,
   build the Jira client, the SSE hub and the poller (which loads the stored snapshot), build the
   handler with `api.New`, serve, start the poller, and open the browser or print the URL to
   stderr. Rotation runs every 60 s. With `--fake` (e2e build) the fakes start first and secrets are in
   memory.
5. Browser: `/auth/launch/#t=...` loads the static page, which clears the fragment with
   `history.replaceState`, posts `{launchToken}` to `POST /api/session`, and on `204` calls
   `location.replace('/')`. `/` calls `GET /api/session` and shows the dashboard, or "Open the app
   with scripts/start.sh" on `401`. `/accounts/` shows "Signed in" and the version.
6. SIGINT or SIGTERM: `srv.Shutdown` with a 10 s limit (the SSE hub closes its streams first), stop the poller and rotation, remove `launch.token`,
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
- The HTTP server sets `ReadHeaderTimeout` 10 s and `ReadTimeout` 30 s. `WriteTimeout` stays unset
  on purpose: a write deadline would cut off the long-lived SSE stream (`/events`). The hub sets a
  deadline per write instead. Handlers that must not hang rely on the request context and the outbound client
  timeouts instead.
- The web app has no inline script beyond what Next emits, no external fonts or CDNs, and no
  `dangerouslySetInnerHTML` (ESLint enforces it).
- Generated code (`server/internal/api/gen`, `web/src/api/schema.d.ts`) is committed and never
  edited by hand. `pnpm gen` regenerates it, and CI fails when the result differs.
- Every merged change bumps `VERSION` and `CHANGELOG.md` (checked by `scripts/check-version.mjs`).

## Tests

- [server/cmd/prdash/main_test.go](../../server/cmd/prdash/main_test.go): flags, data dir modes, private file refusal, browser commands, start, shutdown cleanup, second instance reading the port from the launch file, busy port, bind address, server timeouts, restart with sessions kept, legacy key import, no-terminal exit, file backend.
- [e2e/](../../e2e/playwright.config.ts): Playwright specs, see [auth.md](auth.md).
- [server/internal/api/static_test.go](../../server/internal/api/static_test.go): static serving, redirect, 404 page, empty embed fallback.
- [server/internal/api/api_test.go](../../server/internal/api/api_test.go): launch flow end to end.
- [web/src/lib/launch.test.ts](../../web/src/lib/launch.test.ts), [web/src/app/accounts/page.test.tsx](../../web/src/app/accounts/page.test.tsx), [web/src/app/auth/launch/page.test.tsx](../../web/src/app/auth/launch/page.test.tsx), [web/src/api/client.test.ts](../../web/src/api/client.test.ts).
- [scripts/check-docs.test.mjs](../../scripts/check-docs.test.mjs) and [scripts/check-version.test.mjs](../../scripts/check-version.test.mjs) (`pnpm test:scripts`).
