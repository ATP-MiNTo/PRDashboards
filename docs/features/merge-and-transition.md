# Merge and transition

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 7.7. Related: [review-actions.md](review-actions.md), [status-sync-rules.md](status-sync-rules.md), [merge-method.md](merge-method.md), [command-table.md](command-table.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [settings.md](settings.md).

## Purpose

Merges one pull request on GitHub and moves its Jira tickets in one action, with every check done
again on the server against fresh data. A failed ticket move never undoes the merge: it is kept and
can be retried. Every step is logged in the activity log.

Differences from the spec:

- The panel preselects the transition of the first matching `merged` status sync rule (`SnapshotTicket.mergePlan`), once the transitions of that ticket are loaded, and the person still confirms it. A mode All rule that still waits on other pull requests shows "Won't move yet: waiting on ..." and starts at "Don't move", and the dropdown can still move the ticket. A ticket without a plan starts at "Don't move" ([status-sync-rules.md](status-sync-rules.md)).
- Transitions are fetched in the browser only for a ticket the person chooses to move ("Move...") or that has a merge plan, so opening the panel of a ticket without a plan costs no Jira call. They are cached for 60 s.
- The `merged` rule fingerprints are written (`rule_firings`) in the merge transaction, as the sync engine would compute them with this pull request counted as merged (`Poller.MergedFirings`), so the engine does not move the same ticket again.

## Data model

- `idempotency(key, pr_id, result_json, created_at)`: the result of a finished merge by `Idempotency-Key`, kept 24 hours (purged in the same transaction as each new row). [migration 0002](../../server/internal/store/migrations/0002_merge.sql).
- `activity` (now with `summary`), `pending_retries` (now with `pr_id` and `to_status`), `rule_firings` ([storage.md](storage.md)).
- API: `POST /api/prs/{id}/merge` (`MergeRequest {method, expectedHeadSha, deleteBranch, tickets[{key, transitionId|null, fields}]}`, at most 20 tickets, header `Idempotency-Key` UUID) answers `MergeResult {prId, method, sha, steps[]}`. A step is `{kind: merge|delete_branch|transition, ticket, state: ok|failed|skipped|queued, message, toStatus, retryId}`. A refusal is `MergeProblem` (a problem with `pr`, the fresh state, for 409).
- `GET /api/retries`, `POST /api/retries/{id}` (answers `RetryResult {step}`), `DELETE /api/retries/{id}`, `GET /api/activity?limit&cursor` (newest first, limit 1 to 100, `nextCursor` is the last id of the page or null).
- The order of the review checks is: changes requested ("changes were requested"), approvals ("Needs N approvals, has M", counted from the reviews of the fresh pull request against the repo's `approvalsRequired`), the merge state, a required review. All are `not_mergeable`; the web's `blockOf` applies the same rule to `SnapshotPr.approvals`.
- Problem codes: 409 `head_moved`, `not_mergeable`, `merge_failed`, `merge_pending`, `stack_base_open`, `draft`, `not_open`, `transition_unavailable`, `not_connected`; 404 `not_found`; 422 `method_not_allowed`, `ticket_not_linked`, `missing_field`, `invalid_field`, `idempotency_key_reused`; 429 `rate_limited`; 403 `org_blocked`, `missing_permission`; 502 `upstream_error`.

## Code map

- [server/internal/merge/flow.go](../../server/internal/merge/flow.go): `Service.Merge`, `once`, `run`, `queued`, `check`, `prepare`, `deleteBranch`, `transition`, `Retry`. [fields.go](../../server/internal/merge/fields.go): `encodeFields`. [types.go](../../server/internal/merge/types.go): requests, results, errors and the interfaces it needs.
- [server/internal/github/pr.go](../../server/internal/github/pr.go): `PullRequest` (node query), `Merge` (REST, 405, 409 and 403 mapped; a stack refusal goes on to `MergeAsync`), `DeleteBranch`. [merge_async.go](../../server/internal/github/merge_async.go): `MergeAsync` and the polling. [pull_request.graphql](../../server/internal/github/pull_request.graphql) shares `PullRequestFields` with the search.
- [server/internal/jira/jira.go](../../server/internal/jira/jira.go): `Transition` (POST, drops the cached transitions of the ticket).
- [server/internal/store/merges.go](../../server/internal/store/merges.go): `RecordMerge`, `Idempotent`, `Retries`, `Retry`, `DeleteRetry`, `FinishRetry`, `Activity`.
- [server/internal/api/handlers_merge.go](../../server/internal/api/handlers_merge.go): `MergePr`, `ListRetries`, `RetryTransition`, `DismissRetry`, `GetActivity`.
- [web/src/components/CommandTable/MergePanel.tsx](../../web/src/components/CommandTable/MergePanel.tsx), [MergeSteps.tsx](../../web/src/components/CommandTable/MergeSteps.tsx), [RecentMerges.tsx](../../web/src/components/CommandTable/RecentMerges.tsx); [web/src/lib/merge.ts](../../web/src/lib/merge.ts) (`mergeLabel`, `mergeBlock`); `useMerge`, `useRetryTransition`, `useTicketTransitions` in [queries.ts](../../web/src/api/queries.ts).
- [server/internal/fake](../../server/internal/fake) (e2e build tag): node query, REST merge, the asynchronous merge (config `stacked`, `asyncResult`) and branch delete, Jira transition POST.

## Key flows

1. The Merge button of a ready row opens the panel under the row. The panel remembers the head SHA of the row and makes an `Idempotency-Key` with `crypto.randomUUID`.
2. Every linked ticket is listed with "Don't move". "Move..." loads that ticket's transitions, then a dropdown chooses one. Required fields of the transition render inline (a list for fields with allowed values, text otherwise). Each moved ticket needs its confirm checkbox. With no ticket moved the button reads "Merge without ticket" and needs its own confirmation.
3. The button reads "Merge & move to <status>" for one moved ticket, "Merge & update N tickets" for several. It is disabled, with the reason beside it, while a row block (`blockOf`), a missing required field or a missing confirmation applies.
4. `Merge` runs the steps of spec 7.7: an `Idempotency-Key` that was already used returns the stored result (a key used for another PR is `idempotency_key_reused`; two requests with the same key at once share one run). Then one GraphQL node query and the checks (open, not a draft, stack base not open, head equals `expectedHeadSha`, mergeable, the repo's approvals met ("Needs N approvals, has M", [review-actions.md](review-actions.md)), no changes requested, clean or has hooks, no review missing), the repo allows the method, every requested transition is in the ticket's current list with its required fields.
5. `PUT /repos/{o}/{r}/pulls/{n}/merge` with `merge_method` and `sha`. 409 is `head_moved`, 405 is `not_mergeable`; both carry the fresh state and start a refresh.
   A 403 whose message says stacked PRs must use the asynchronous merge endpoint is not a permission problem: see "Asynchronous merge" below.
6. Branch delete when asked: only a branch of the same repo, and not one another open PR uses as its base ("Branch kept: ..." otherwise). A failed delete is a failed step, nothing more.
7. Tickets are moved at most 4 in parallel. A failure becomes a `pending_retries` row and a step with `retryId`.
8. One transaction (`RecordMerge`) writes the activity rows, the retries, the rule firings and the idempotency row. A `activity` event is published and a refresh is triggered. The web shows the steps in the panel, and in "Recent merges" once the row has left the table.
9. Retry (`POST /api/retries/{id}`) checks again that the transition is still offered, posts it, logs it and deletes the retry row. Dismiss (`DELETE`) removes the row.
10. A `head_moved` answer blocks the panel until it is closed and opened again, so the person sees the changed row first.

## Outside calls

- Per merge: one GraphQL node query, one REST merge, at most one ref delete.
- Jira: `GET transitions` only for tickets being moved and not cached (the browser's own lookup fills the 60 s cache), one POST per moved ticket. Nothing for tickets left at "Don't move".
- Retry: one transitions lookup (cached) and one POST.

## Rules and invariants

- The server never trusts the browser: head SHA, mergeable state, stack base and transitions are checked against fresh data. Branch protection is never bypassed.
- The merge is never rolled back. After GitHub merged, nothing may cancel the remaining steps (the context is detached with its own timeout).
- Outbound calls only through `netguard`. Tokens never appear in steps, activity or logs. Jira's 400 explanation is cut to 200 characters.
- All the database writes of one merge are one transaction.
- Field values are shaped by the transition's own metadata (`{"id"}` for options, `{"accountId"}` for users, text as is). Unknown ids and unsupported types are refused.

### Asynchronous merge

GitHub merges a pull request of a GitHub-native stack only through its asynchronous merge API (merging one also merges every unmerged pull request below it, in one step; the pull request above is rebased onto the base by GitHub). The synchronous `PUT .../merge` answers a stacked pull request with 403 "Merging stacked PRs via this endpoint is not supported. Use the asynchronous merge endpoint instead.". `Merge` recognises exactly that (a message with "stacked PRs" and "asynchronous merge endpoint", any case) and calls `MergeAsync` once; every other 403 is classified as before. Normal pull requests keep the synchronous call.

`MergeAsync` sends `PUT /repos/{o}/{r}/pulls/{n}/merge-async` with `{merge_method, sha}` (the head pin stays) and `X-GitHub-Api-Version: 2026-03-10` on every call, then polls `GET .../merge-async/{id}`, waiting 1 s, 2 s, 4 s and then 5 s between polls, for at most 90 s overall (`Backoff.Wait`, so tests do not sleep). The id is taken from a 202 or a 409 ("a request already exists") and must match `^[A-Za-z0-9-]{1,64}$` before it goes into a URL. Outcomes:

| GitHub says | Client result | API |
| --- | --- | --- |
| `merged` | `MergeResult` (SHA when GitHub names it, else empty) | 200, the normal flow: branch rule, tickets, firings |
| `enqueued` (merge queue) | `ErrEnqueued` | 200, merge step `queued` "Added to the merge queue", activity "Added to the merge queue: ...", tickets are not moved and the branch is kept; the poller's `merged` rule moves the tickets once it merges. The result is stored under the idempotency key |
| `failed` | `*MergeFailedError{Reason}` (200 characters) | 409 `merge_failed`, detail "GitHub did not merge: <reason>" (plain text), nothing moves |
| still `pending` at 90 s | `ErrMergePending` | 409 `merge_pending` "GitHub is still merging; refresh in a moment", nothing moves; a refresh is started and the poller handles the merge when it lands |
| 400, 422 | `ErrNotMergeable` | 409 `not_mergeable` |
| 404 | `ErrPRNotFound` | 404 |
| 403 | as in "Refusals by GitHub" | 403 `org_blocked` or `missing_permission` |

The documentation does not give the poll body in full, so the client reads `status` (top level, else `details.status`), the failure reason from `message`, `details.message`, `details.reason` or `reason`, and the merge commit from `details.merge_commit_sha`, `merge_commit_sha` or `sha` (kept only when it looks like a commit id). The id may be in `details.uuid`, `uuid` or `id`. An unknown status is treated as pending, and a 5xx while polling is waited out until the cap. The HTTP request of the merge stays open while polling (the server has no write timeout).

### Refusals by GitHub

GitHub says no in three different ways, and the client tells them apart ([github.go](../../server/internal/github/github.go) `classifyForbidden`):

- SAML SSO (`X-GitHub-SSO: required`, or a message with "SAML"): `*OrgBlocked{Reason: "sso"}`, 403 `org_blocked`, authorize link when the organization is known.
- Organization policy (OAuth App access restrictions, a ban on classic or fine-grained tokens, a token lifetime limit, an IP allow list; a small table of message fragments, `policyMatchers`): `*OrgBlocked{Reason: "policy"}`, 403 `org_blocked` with the detail "The <org> organization policy does not allow this token". GitHub's own text is kept in the error and the log, never sent to the browser.
- Anything else FORBIDDEN or 403, such as "Resource not accessible by personal access token": `*PermissionError{Message, Accepted}`, 403 `missing_permission`. `Accepted` comes from `X-Accepted-GitHub-Permissions` (only entries shaped `name=read|write|admin`). The detail reads "The token needs the GitHub permission contents: write" (or "needs one of these ..."; without the header, "The token lacks a GitHub permission this action needs: <message>"). The web shows this detail as plain text.
- A secondary rate limit 403 stays a `RateLimitedError` (checked first).

Where it applies: `PullRequest` (the re-check) returns OrgBlocked for SSO and policy always. A permission error is returned only when no node came back; with the node present (for example a FORBIDDEN on `reviewRequests` because the token has no organization Members permission) the pull request is returned and the merge call itself reports a missing permission. REST `Merge` and `DeleteBranch` classify a 403 the same way. A `PermissionError` from the merge stops the flow; from the branch delete it is a failed step with the same sentence and the merge stays. Search records permission errors in `SearchResult.Errors` and adds no blocked-organization banner ([poller.md](poller.md)).

Every refusal by GitHub is logged at WARN by the client (`github refused a call`): operation, status, message (200 characters), GraphQL error type and path, accepted permissions. Never the token, the request or the whole response.

## Tests

- [server/internal/merge](../../server/internal/merge): `flow_test.go` (every refusal, the approvals rule, partial failure, retry, idempotent replay, concurrent same key, parallelism of 4, branch rules), `calls_test.go` (outbound call counts and one `RecordMerge`), `resolve_test.go`.
- [server/internal/github/merge_async_test.go](../../server/internal/github/merge_async_test.go) (stack message detection, submit answers, id validation, polling outcomes and cap, header, cancel), [server/internal/merge/async_test.go](../../server/internal/merge/async_test.go) (enqueued, failed, pending), [server/internal/github/pr_test.go](../../server/internal/github/pr_test.go), [permission_test.go](../../server/internal/github/permission_test.go) (classification, header parsing, REST and node refusals, logging), [server/internal/jira/jira_test.go](../../server/internal/jira/jira_test.go), [server/internal/store/merges_test.go](../../server/internal/store/merges_test.go), [server/internal/api/merge_api_test.go](../../server/internal/api/merge_api_test.go).
- [MergePanel.test.tsx](../../web/src/components/CommandTable/MergePanel.test.tsx), [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx).
- [e2e/merge.spec.ts](../../e2e/merge.spec.ts): merge and move with a required field, a failed move that is retried, a head that moved, a token without Contents write (fake `github_pat_contents_read`), a stacked pull request merged through the asynchronous API.
