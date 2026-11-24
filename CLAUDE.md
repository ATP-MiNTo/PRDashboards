# PR Dashboards

A local dashboard for every open pull request assigned to me across GitHub, linked to Jira
tickets, with merge and Jira status sync. Background: [docs/PRD.md](docs/PRD.md),
[docs/TECH_SPEC.md](docs/TECH_SPEC.md) and [docs/architecture.md](docs/architecture.md).

## Repo map

- `api/openapi.yaml` - API contract. Go and TS types are generated from it, never edited by hand.
- `server/` - Go module. `cmd/prdash` (main), `internal/{api,auth,secrets,store,github,jira,linking,stacks,rules,merge,poller,events,netguard,fake}`.
- `web/` - Next.js static export (React, TypeScript, TanStack Query), embedded into the binary.
- `e2e/` - Playwright specs for boundary flows only.
- `scripts/` - `start.sh`, `start.command`, `start.cmd` and check scripts.
- `docs/` - PRD, tech spec, architecture and one doc per feature in `docs/features/`.

## Feature docs

- `docs/features/<feature>.md` is the source of truth for each feature, and `docs/architecture.md`
  for cross-cutting rules and the feature index.
- Read the feature docs a task touches before starting, and follow them.
- A change to a feature's behaviour, data, flows, files or tests updates its doc in the same PR.
- A new feature adds a doc from the template in `docs/features/README.md` and updates the index.
- When code and doc disagree, fix the one that is wrong in the same PR.

## Commands

Not built yet. Planned:

```sh
scripts/start.sh            # build if needed, run, open the browser
go test -race ./server/...  # server tests
pnpm -C web test            # UI tests
pnpm -C web build           # static export
pnpm e2e                    # Playwright against the e2e build
```

## Security

- The browser is a remote client. Every request passes Host, Sec-Fetch-Site, Origin, session,
  CSRF, body size and schema checks. No CORS. Bind `127.0.0.1` only.
- Tokens and client secrets only in the keychain or the encrypted fallback. Never in SQLite, logs
  or API responses, never sent to the browser.
- Every write re-checks state on the server against fresh data (head SHA, mergeable, transitions).
- No generic proxy endpoint. Outbound calls only through `netguard`.
- No `dangerouslySetInnerHTML`. Build links from validated parts.
- Test fakes only behind the `e2e` build tag.

## Calls and storage

- No N+1 calls: one paginated GraphQL search for PRs, one JQL per 100 ticket keys, transitions
  only for tickets being moved.
- One refresh at a time. Browser tabs never trigger outside calls on their own.
- One SQLite writer. Multi-row changes in one transaction. No per-row loops against the database.

## Package boundaries

`rules`, `linking`, `stacks` and the merge method resolver are pure: no network, no database, no
clock. Only `github`, `jira` and `auth` make outbound calls. Only `store` touches SQLite.

## Testing

- Go table tests for pure packages, 90% line coverage gate. Provider clients against recorded
  fixtures with `httptest`. All Go tests with `-race`. No test calls real GitHub or Jira.
- Vitest + Testing Library for key UI.
- Playwright only for flows that cross into GitHub or Jira (OAuth, SSO blocks, rate limits, merge
  and transition, head moved), against the `e2e` build with fake servers.

## Versioning

Every merged change bumps `VERSION` and adds a dated entry to `CHANGELOG.md`. No "Unreleased"
section.

## Commits and pull requests

- Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`, `refactor:`).
- Branch names: `type/PRD-<n>-title-in-kebab-case`.
- PR body: ticket line, then `# Overview`, `# Change`, and a `Note:` line.
