# Review actions and approvals

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 7.7, 8 and 9. Related: [merge-and-transition.md](merge-and-transition.md), [settings.md](settings.md), [poller.md](poller.md), [security.md](security.md), [auth.md](auth.md), [command-table.md](command-table.md).

## Purpose

Lets the person approve a pull request, request changes on it and take a draft out of draft from the
dashboard, and makes the number of approvals a repository requires part of the merge rules. The
server side and the web plumbing (API client functions and mutation hooks) are built here; the
controls on the cards (the Approve and Request changes dropdown, the Ready button and the approvals
counts) use them and are described in [command-table.md](command-table.md).

Differences from the spec: this is not in the PRD or the spec. It follows the pattern of the merge
([merge-and-transition.md](merge-and-transition.md)): the browser asks, the server checks again
against fresh data and acts as the viewer.

## Data model

- API: `POST /api/prs/{id}/approve` (`ApproveRequest {expectedHeadSha, body?}`), `POST /api/prs/{id}/request-changes` (`RequestChangesRequest {expectedHeadSha, body}`, `body` 1 to 65536 characters and not only white space) and `POST /api/prs/{id}/ready` (`ReadyRequest {expectedHeadSha}`) answer `ReviewResult {prId, action: approve | request_changes | ready}`. `expectedHeadSha` is 7 to 64 hex characters, the head the card showed. GitHub's limit for a review text is 65536 characters; the 64 KiB request cap ([security.md](security.md)) is checked first, so the largest text that arrives is a little shorter.
- Refusals: 409 `ReviewConflict` (a `MergeProblem`, with the fresh pull request in `pr`): `not_open`, `head_moved`, `not_draft`, `own_pull_request`, `not_connected`. 404 `not_found`. 422 `body_required` (white space only) and `review_rejected` (GitHub refused for another reason; the detail is "GitHub refused it: <GitHub's text, 200 characters>"). 403 `missing_permission` and `org_blocked`, 429 `rate_limited`, 502 `upstream_error`.
- Settings: `RepoSettings.approvalsRequired` (1 to 10, `ApprovalsRequired`), `RepoUpdate.approvalsRequired` (optional: left out, the stored value stays). The column is `repos.approvals_required INTEGER NOT NULL DEFAULT 1 CHECK (BETWEEN 1 AND 10)`, added by [migration 0004](../../server/internal/store/migrations/0004_approvals.sql), so a database from before it reads 1 for every repo. It is per repository only, not per target branch. [storage.md](storage.md), [settings.md](settings.md).
- Snapshot: `SnapshotPr.approvals {have, need, changesRequested}`. `have` is the number of distinct reviewers whose latest review is `APPROVED`, `changesRequested` the number whose latest review is `CHANGES_REQUESTED`, `need` the repo setting. Always present. A snapshot stored before it existed gets it computed from its stored reviews and the stored setting when it is loaded (`fillApprovals`), until the next refresh.

## Code map

- [server/internal/merge/approvals.go](../../server/internal/merge/approvals.go): the pure part. `CountApprovals`, `Approvals.Met`, `Approvals.Block` ("Needs N approvals, has M"), `NeedOf` (a value outside 1 to 10 is 1). No network, database or clock; covered by [approvals_test.go](../../server/internal/merge/approvals_test.go).
- [server/internal/merge/flow.go](../../server/internal/merge/flow.go) `check`: refuses the merge with `not_mergeable` and the `Block` text when `have < need`, counted from the reviews GitHub just reported. `need` comes from `Linked.ApprovalsRequired`, which the poller fills from the current snapshot ([poller.go](../../server/internal/poller/poller.go) `Linked`).
- [server/internal/review/review.go](../../server/internal/review/review.go): `Service.Approve`, `RequestChanges`, `MarkReady`, and the re-check (`check`). Refusals are `*merge.Error` values, so the API maps them like the merge's.
- [server/internal/github/review.go](../../server/internal/github/review.go) and [review.graphql](../../server/internal/github/review.graphql): `SubmitReview` (REST) and `MarkReady` (GraphQL mutation), error mapping, `ErrOwnPullRequest`, `ErrNotDraft`, `ReviewRejectedError`.
- [server/internal/api/handlers_review.go](../../server/internal/api/handlers_review.go): `ApprovePr`, `RequestChangesPr`, `MarkPrReady`; `providerFailure` in [handlers_merge.go](../../server/internal/api/handlers_merge.go) maps GitHub's refusals for the merge and the review actions alike.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): `approvalsOf`, `fillApprovals`; [store/repos.go](../../server/internal/store/repos.go): `ApprovalsRequired` in `RepoConfig`, `PutRepo` and `PutRepos`.
- [server/internal/fake](../../server/internal/fake) (e2e build tag): `POST .../pulls/{n}/reviews`, the ready mutation, the config `review` (acme/review #31 with one approval, #32 a draft, #33 opened by the viewer) and the token `github_pat_pulls_read` (Pull requests read only).
- Web: [web/src/api/client.ts](../../web/src/api/client.ts) `approvePr`, `requestChangesPr`, `markPrReady`; [queries.ts](../../web/src/api/queries.ts) `useApprove`, `useRequestChanges`, `useMarkReady`, `useSaveApprovals`, `approvalsUpdate`; [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts) `approvalsBlock` and `blockOf`.

## Key flows

1. The card sends the action with the head SHA it shows (and the text, for a request for changes). The router has already applied the checks of [security.md](security.md): Host, Origin, Sec-Fetch-Site, session, CSRF, content type, body size and schema.
2. `Service.run`: the pull request must be in the current snapshot (`not_found` otherwise), the GitHub token is read (`not_connected`), and one GraphQL node query fetches the pull request again.
3. `check` against the fresh data: open (`not_open`), a draft when marking ready (`not_draft`), head equal to `expectedHeadSha` (`head_moved`), and for approve and request changes not opened by the connected user (`own_pull_request`, compared case-insensitively with the stored login of the account; GitHub refuses it too, and that refusal is mapped to the same code). A refusal carries the fresh state, and starts a refresh so the card catches up.
4. The write: approve and request changes are `POST /repos/{o}/{r}/pulls/{n}/reviews` with the event, the text and `commit_id` set to `expectedHeadSha`, so GitHub itself refuses a commit that is no longer the head (`head_moved`). Mark ready is the GraphQL mutation `markPullRequestReadyForReview` and succeeds only when GitHub answers with the pull request no longer a draft. The viewer's own token is used.
5. After a success `Refresh` starts one refresh (`Poller.Trigger`, joining a running one), the same call a merge makes. It is the usual two searches, so there is no call per pull request and no polling of its own; the `snapshot` event it publishes is what the browser already listens to. The web hooks also invalidate the snapshot query when the request ends, whatever the outcome.
6. Merge: `check` counts the approvals of the fresh pull request against `Linked.ApprovalsRequired`. The card's Merge cell uses `blockOf`, which applies the same rule to `SnapshotPr.approvals` with the same words, after "Changes requested" and before "Review required".

## Outside calls

- Per action: one GraphQL node query (the re-check) and one write: one REST review POST, or one GraphQL mutation. Then one refresh (two searches, [poller.md](poller.md)).
- Nothing from the browser to GitHub. Saving the approvals setting makes no outside call: it rebuilds the snapshot from cached data (`Relink`).

## Rules and invariants

- Every write re-checks fresh state on the server; the client's view is only an expectation (the head SHA). The head SHA is also sent to GitHub as `commit_id`.
- Permission errors name the permission: a 403 that is not SSO or an organization policy is `missing_permission` with the detail "The token needs the GitHub permission pull_requests: write". GitHub's `X-Accepted-GitHub-Permissions` header is used when it comes; the GraphQL mutation and a REST answer without the header get `pull_requests=write` filled in (`needsReviewPermission`). A fine-grained token needs Pull requests: write; a GitHub App needs the same ([auth.md](auth.md)). GitHub's own text is kept in the log, not in the detail.
- No idempotency key: an approval repeated is harmless (the latest review of a reviewer is what counts), and a repeated request for changes is a second review by the same reviewer, which GitHub folds into the first.
- Approvals count only the latest review of each reviewer that GitHub returns in `latestReviews(first: 10)`. A reviewer whose latest review is a comment is not counted, and only the 10 most recent reviewers are seen, which is the most `approvalsRequired` can be.
- `merge.CountApprovals`, `Approvals.Block` and `NeedOf` stay pure and are covered at 90 % or more (`pnpm coverage-check`).
- The review actions are logged at INFO (`review action done`: pull request and action, no text); no activity row is written.
- The CI "cold restart" of the design is a UI-only notion; nothing here touches CI.

## Tests

- [server/internal/merge/approvals_test.go](../../server/internal/merge/approvals_test.go) (the count, the limits, the text), [flow_test.go](../../server/internal/merge/flow_test.go) (merge refused with too few approvals, "Needs 2 approvals, has 1", allowed with enough).
- [server/internal/review/review_test.go](../../server/internal/review/review_test.go): the re-check, pinning, error mapping, one refresh. [server/internal/github/review_test.go](../../server/internal/github/review_test.go): the REST and GraphQL calls and every refusal.
- [server/internal/api/review_api_test.go](../../server/internal/api/review_api_test.go): the three endpoints (validation, guards, 409 with the fresh state, permission and organization problems); [merge_api_test.go](../../server/internal/api/merge_api_test.go) `TestRepoApprovalsRequired`; [server/internal/store/repos_test.go](../../server/internal/store/repos_test.go); [server/internal/poller/approvals_test.go](../../server/internal/poller/approvals_test.go) (the count in the snapshot, an older stored snapshot).
- [server/internal/fake/github_review_test.go](../../server/internal/fake/github_review_test.go): the fake's endpoints.
- [web/src/api/client.test.ts](../../web/src/api/client.test.ts), [web/src/api/queries.test.tsx](../../web/src/api/queries.test.tsx), [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts).
- [e2e/review.spec.ts](../../e2e/review.spec.ts): through the page, with the CSRF header, against the fake: a repo that needs two approvals blocks the merge until the second one, request changes (text required, head checked), mark ready once, a pull request the viewer opened, a token without Pull requests write.
