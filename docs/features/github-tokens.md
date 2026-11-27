# Several GitHub tokens

Status: Partial. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 5, 7 and 10. Related: [auth.md](auth.md), [storage.md](storage.md), [poller.md](poller.md), [security.md](security.md), [merge-and-transition.md](merge-and-transition.md), [review-actions.md](review-actions.md), [settings.md](settings.md), [sidebar-shell.md](sidebar-shell.md).

## Purpose

The server side is built. The Accounts dialog still shows one token, and the web work is separate.

One dashboard reads and operates on repositories of different owners. A fine-grained token or a GitHub App
installation belongs to one owner (a user or an organization), so a person who works in `acme` and in
`oss` needs a token from each. Up to 10 GitHub tokens can be connected, each with a name. The poller runs the
searches with every token, merges the results by pull request, and picks one token for each repository:
the one whose account has the highest role there. Merge, review, ready and branch deletion use the token
of the pull request's repository.

This is not in the PRD or the spec. It changes the one-token rule of the auth and poller docs.

- There is a primary token, the first of the list. The summary `Accounts.github` describes it, a GitHub App
  or OAuth sign-in replaces the App or OAuth entry in place (at most one of those), and a personal access
  token is always added.
- The API is complete. The Accounts dialog still has the single token UI (its "Replace token" form calls
  `PUT /api/github/pat`, which now adds a token when one is stored), until the web change lands.

## Data model

- Secret `github.tokens` in the vault ([storage.md](storage.md)): a JSON array, primary first, of
  `{id, name, token, method (app, pat, or legacy oauth), fineGrained, login, warning, refreshToken,
  expiresAt, needsReconnect}`. `id` is 8 hex characters, unique in the list and never reused for a token that
  is still stored. `name` is 1 to 40 characters without control characters, unique ignoring case. A token
  without a name gets `Token (login)`, `GitHub App (login)` or `OAuth (login)`, made unique with a number.
  Tokens never leave the vault: no API response, log or snapshot has one.
- Migration: the legacy item `github.token` (one object, no id or name) becomes the one entry of the list,
  named by the default rule, on the first read of the list (`tokensLocked`) and at start
  (`OAuth.MigrateGitHub`, called by `run`). The legacy item is deleted after the list is written. A failed
  delete leaves a copy that is never read again while the list exists. An empty list deletes both items.
- Limits and refusals: at most 10 tokens (`token_limit`, 409), the same token string twice
  (`token_duplicate`, 409, compared in constant time, the detail names the stored token), a name used by
  another token (`token_name_taken`, 409), an invalid name (422 `invalid_body`).
- API schemas (from [openapi.yaml](../../api/openapi.yaml)): `GithubToken` (`id` pattern `^[0-9a-f]{8}$`,
  `name`, `method` (`app`, `oauth`, `pat`), `fineGrained`, `login`, `warning` (nullable), `needsReconnect`),
  `TokenName`, `AddTokenRequest` (`token`, optional `name`), `RenameTokenRequest` (`name`), `PatRequest` (optional
  `name`), `Accounts.githubTokens` (at most 10) and `Accounts.githubTokenLimit` (10), `SnapshotRepo.tokenId`
  and `SnapshotRepo.tokenName`. `Accounts.github` stays as the summary: `connected` when any token is stored,
  `login`, `method` and `warning` of the primary token, `needsReconnect` when any token needs it.
- `SnapshotRepo.viewerPermission` is now the role of the chosen token's account ([poller.md](poller.md)).

| Method and path | Purpose |
| --- | --- |
| `PUT /api/github/pat` | Setup flow, kept: the first token becomes the primary, later ones are added like `POST /api/github/tokens` (200, the summary) |
| `POST /api/github/tokens` | Validate and add a token, `{token, name?}` (201, `GithubToken`) |
| `PUT /api/github/tokens/{id}` | Rename, `{name}` (200, `GithubToken`; 404 unknown id, 409 `token_name_taken`) |
| `DELETE /api/github/tokens/{id}` | Remove one (204; 404 unknown id). Removing the last one disconnects GitHub |
| `POST /api/github/tokens/{id}/check` | Check one token with GitHub now (200, `Accounts`; 404, 422, 502) |
| `POST /api/accounts/github/check` | Check every token (all are checked even when one fails) |
| `DELETE /api/accounts/github` | Disconnect: remove every token |
| `GET /api/accounts` | Stored state only, with `githubTokens` and `githubTokenLimit` |

An add, rename or remove wakes the poller (`kick`), so the dashboard follows the new set of tokens.

## Code map

Server:

- [server/internal/auth/github_tokens.go](../../server/internal/auth/github_tokens.go): the list in the vault. `MaxGitHubTokens`, `ValidName`, `admit`, `defaultName`, `findToken`, `tokensLocked` (migration), `MigrateGitHub`, `saveTokens`, `saveSignIn`, `GitHubTokens`, `RenameGitHubToken`, `RemoveGitHubToken`, the errors and `DuplicateError`.
- [server/internal/auth/github.go](../../server/internal/auth/github.go): `AddPAT`, `GitHubAccess(ctx, id)` (empty id is the primary), `GitHubCredential`, `refreshGitHub` for one entry, `CheckGitHub`, `CheckGitHubToken`, `MarkGitHubRejected`.
- [server/internal/tokenpick/tokenpick.go](../../server/internal/tokenpick/tokenpick.go): the pure rules. `Rank`, `Choose`, `Merge`, `Lowest`, `Tightest`. No network, database or clock; covered by the 90% gate.
- [server/internal/poller/refresh.go](../../server/internal/poller/refresh.go): `fetchGitHub`, `failGitHub`, `resolveLeft` per token. [poller.go](../../server/internal/poller/poller.go): `Accounts`, `ghData.tokens`, `rl` and `rlShown`, `Linked`. [build.go](../../server/internal/poller/build.go): `SnapshotRepo.tokenId` and `tokenName`.
- [server/internal/merge/flow.go](../../server/internal/merge/flow.go) and [server/internal/review/review.go](../../server/internal/review/review.go): take the token of `Linked.TokenID`.
- [server/internal/github/github.go](../../server/internal/github/github.go): `PermissionError.Token`, `WithToken` and the detail that names it.
- [server/internal/api/handlers_tokens.go](../../server/internal/api/handlers_tokens.go): the five handlers, `tokenProblem`. [handlers_accounts.go](../../server/internal/api/handlers_accounts.go): `githubSummary`, `accounts`.
- [server/cmd/prdash/accounts.go](../../server/cmd/prdash/accounts.go): `GitHubTokens` (no token values), `GitHubToken` (falls back to the primary), `GitHubRejected`.
- [server/internal/fake/github.go](../../server/internal/fake/github.go) and [github_search.go](../../server/internal/fake/github_search.go) (e2e tag): the two owner tokens and the call log.

## Key flows

1. Add: `POST /api/github/tokens` (or `PUT /api/github/pat`) calls `handlers.addPAT`, then `OAuth.AddPAT`. It
   checks the name, then the limit, the duplicate and the name against the list, before it calls GitHub.
   Then `Viewer`, the scopes (a classic token needs `repo` and `read:org`, a fine-grained one is stored with
   `fineGrained` and the warning). Under `tokenMu` it checks the list again (it may have changed while
   GitHub answered), assigns the id and the name, and writes the list.
2. App sign-in: `completeGitHub` calls `saveSignIn`. A new sign-in replaces the App or OAuth entry in place
   and keeps its id, position and name (a default name follows the new login). Without one the entry is added,
   and at 10 tokens `CompleteOAuth` answers 409 `token_limit`.
3. Refresh (poller): `fetchGitHub` lists the credentials (`Accounts.GitHubTokens`, no calls), then for each
   one, in list order, gets a valid token (`GitHubToken`, refreshing an App token when due) and runs the
   three searches. A credential removed since the list was read is skipped. `tokenpick.Merge` joins the
   results (below). Then `resolveLeft` runs the merged search per token when pull requests left the list.
4. Choice: for each repository the credential with the highest `viewerPermission` among those whose
   searches returned it (`ADMIN` > `MAINTAIN` > `WRITE` > `TRIAGE` > `READ` > unknown); a tie goes to
   the earlier one in the list. The pull request data is the chosen token's copy; when the chosen token's
   searches lack that pull request, the copy of the best token that has it. Roles (`assigned`, `authored`,
   `reviewer`) are the union over the tokens, in that order. A blocked organization (SSO) of one token
   is dropped when another token returned pull requests of the same owner.
5. Write: merge, review, ready, async merge and branch delete read `Linked(id).TokenID` from the snapshot and
   call `Accounts.GitHubToken(ctx, tokenID)`. An empty id (a snapshot stored before this change) or an id
   that was removed since falls back to the primary token. The own-pull-request check of a review uses the
   login of that credential. A `PermissionError` gets the repository and the token name (`WithRepo`,
   `WithToken`).
6. Remove: `DELETE /api/github/tokens/{id}` removes the entry, deletes both vault items when the list is
   empty, and wakes the poller. The next refresh leaves out what only that token saw, and a repository that
   another token also sees moves to it (often as `READ`).
7. Rejected token: a refresh that gets `ErrBadCredentials` from one token marks that entry
   `needsReconnect` (`GitHubRejected`) and fails the whole refresh.

## Outside calls

- Poll: three paginated GraphQL searches per connected token, so at most 30 per refresh (10 tokens), still
  no call per pull request. The tokens run one after the other, the three searches of one token together,
  so at most four requests are in flight. When pull requests left the list, one merged search per token.
- Merge and review: the same calls as before, with the token of the repository.
- Add and check: one `viewer { login }` per token, on a person's action only. `POST /api/accounts/github/check`
  makes one per token.
- The same netguard rules, hosts and limits as [auth.md](auth.md).

## Rules and invariants

- Tokens only in the vault. `GET /api/accounts`, the snapshot, logs and problems carry names, logins and
  ids only.
- Every new route passes the whole middleware stack (Host, Sec-Fetch-Site, Origin, session, CSRF, body size,
  schema). A write needs the CSRF header, a body over the limit is refused, and `{id}` must match its pattern
  (`TestTokenRoutesPassTheWholeMiddlewareStack`, `TestProtectedSurface`).
- One `tokenMu` covers every read-modify-write of the list, so a refresh rotation of one App token never
  loses an add or a rename. A refresh or rotation writes its own entry by id.
- If any token fails, the whole refresh fails like it does for one token: the last snapshot stays, and the
  source error names the token: `GitHub token "OSS" was rejected` or `Reconnect GitHub token "OSS"` (state
  `disconnected`), "GitHub rate limit reached" (`rate_limited`) or "GitHub could not be reached" (`error`).
  With one token and no name the texts are the earlier ones (`GitHub rejected the token`, `Reconnect GitHub`).
- Rate limit: the sidebar shows the tightest budget (`rlShown`, the credential with the least remaining) and
  the poller's own backoff uses the lowest remaining (`rl`), so neither hides a token that is nearly out.
- A personal access token flagged `needsReconnect` is still tried, so a token that works again (a lifted
  SSO block, say) recovers without being added again. The flag is for the person; a successful check or refresh
  clears it. An App or OAuth token that is flagged is not used until the person signs in again.
- The summary's `needsReconnect` is true when any token needs it.
- The chosen token is a rule of the snapshot, not a setting: nothing is stored in SQLite.
- `Accounts.github` is not a token: with several tokens its `login`, `method` and `warning` are the primary's.
- Fake (e2e tag only): see Tests.

## Tests

- [server/internal/tokenpick/tokenpick_test.go](../../server/internal/tokenpick/tokenpick_test.go): the selection rule as table tests (permission order, ties, unknown roles, roles union, the chosen copy, blocked orgs, rate limits), 100% coverage.
- [server/internal/auth/github_tokens_test.go](../../server/internal/auth/github_tokens_test.go): migration (and its idempotence), add, rename, remove, the limit, duplicates, names, default names, sign-in replacing, concurrent refresh and add, rejection marks, checks.
- [server/internal/api/tokens_api_test.go](../../server/internal/api/tokens_api_test.go): the five routes with their problem codes, the summary and the list in `GET /api/accounts` (no token in any body), the full middleware stack for every new route.
- [server/internal/poller/tokens_test.go](../../server/internal/poller/tokens_test.go): searches per token, merging, the chosen token and its role, a failing token (named, marked), rate limits.
- [server/internal/merge/tokens_test.go](../../server/internal/merge/tokens_test.go) and [server/internal/review/tokens_test.go](../../server/internal/review/tokens_test.go): the `Authorization` token of each call, the fallback to the primary, the own-pull-request check with the chosen login, the permission detail.
- [server/internal/github/permission_test.go](../../server/internal/github/permission_test.go): `WithToken` and the detail.
- [server/internal/fake/github_tokens_test.go](../../server/internal/fake/github_tokens_test.go) and [tokens_integration_test.go](../../server/internal/fake/tokens_integration_test.go) (e2e tag): the fake itself, and the real auth, poller, merge and review against it with two tokens of different owners: tokens and roles in the snapshot, three searches per token, which token each write used (from the call log), own pull requests, removal, a rejected token.
- Fake GitHub (e2e tag): the PATs `github_pat_acme` (login `octocat`, sees `acme/*`, `acme-corp/*` not, and `oss/lib` as `READ`) and `github_pat_oss` (login `hubot`, sees only `oss/lib`, as `WRITE`). Other fake PATs see everything as before. Calls to a repository a restricted token does not see answer 404, and a write with a `READ` role answers 403 with the accepted permission. `GET /__admin/state` has `calls`, every search, node lookup, merge, review, ready and branch delete with its token and repository.
- Web: no new web tests yet. [AccountsPanel.test.tsx](../../web/src/components/AccountsPanel.test.tsx) has the new fields in its fixture.
