# Merge and transition

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 7.7. Related: [merge-method.md](merge-method.md), [command-table.md](command-table.md), [ticket-linking.md](ticket-linking.md), [stacked-prs.md](stacked-prs.md), [settings.md](settings.md).

## Purpose

Merges one pull request on GitHub and moves its Jira tickets in one action, with every check done
again on the server against fresh data. A failed ticket move never undoes the merge: it is kept and
can be retried. Every step is logged in the activity log.

Differences from the spec:

- Status sync rules do not exist yet (milestone 8), so no transition is preselected: every ticket starts at "Don't move". "Won't move yet" for multi-PR tickets arrives with that milestone.
- Transitions are fetched in the browser only for a ticket the person chooses to move ("Move..."), so opening the panel costs no Jira call.
- The `merged` rule fingerprints are written (`rule_firings`) with the PR id as fingerprint for the tickets handled, so the later sync engine can skip them.

## Data model

- `idempotency(key, pr_id, result_json, created_at)`: the result of a finished merge by `Idempotency-Key`, kept 24 hours (purged in the same transaction as each new row). [migration 0002](../../server/internal/store/migrations/0002_merge.sql).
- `activity` (now with `summary`), `pending_retries` (now with `pr_id` and `to_status`), `rule_firings` ([storage.md](storage.md)).
- API: `POST /api/prs/{id}/merge` (`MergeRequest {method, expectedHeadSha, deleteBranch, tickets[{key, transitionId|null, fields}]}`, at most 20 tickets, header `Idempotency-Key` UUID) answers `MergeResult {prId, method, sha, steps[]}`. A step is `{kind: merge|delete_branch|transition, ticket, state: ok|failed|skipped, message, toStatus, retryId}`. A refusal is `MergeProblem` (a problem with `pr`, the fresh state, for 409).
- `GET /api/retries`, `POST /api/retries/{id}` (answers `RetryResult {step}`), `DELETE /api/retries/{id}`, `GET /api/activity?limit&cursor` (newest first, limit 1 to 100, `nextCursor` is the last id of the page or null).
- Problem codes: 409 `head_moved`, `not_mergeable`, `stack_base_open`, `draft`, `not_open`, `transition_unavailable`, `not_connected`; 404 `not_found`; 422 `method_not_allowed`, `ticket_not_linked`, `missing_field`, `invalid_field`, `idempotency_key_reused`; 429 `rate_limited`; 403 `org_blocked`; 502 `upstream_error`.

## Code map

- [server/internal/merge/flow.go](../../server/internal/merge/flow.go): `Service.Merge`, `once`, `run`, `check`, `prepare`, `deleteBranch`, `transition`, `Retry`. [fields.go](../../server/internal/merge/fields.go): `encodeFields`. [types.go](../../server/internal/merge/types.go): requests, results, errors and the interfaces it needs.
- [server/internal/github/pr.go](../../server/internal/github/pr.go): `PullRequest` (node query), `Merge` (REST, 405 and 409 mapped), `DeleteBranch`. [pull_request.graphql](../../server/internal/github/pull_request.graphql) shares `PullRequestFields` with the search.
- [server/internal/jira/jira.go](../../server/internal/jira/jira.go): `Transition` (POST, drops the cached transitions of the ticket).
- [server/internal/store/merges.go](../../server/internal/store/merges.go): `RecordMerge`, `Idempotent`, `Retries`, `Retry`, `DeleteRetry`, `FinishRetry`, `Activity`.
- [server/internal/api/handlers_merge.go](../../server/internal/api/handlers_merge.go): `MergePr`, `ListRetries`, `RetryTransition`, `DismissRetry`, `GetActivity`.
- [web/src/components/CommandTable/MergePanel.tsx](../../web/src/components/CommandTable/MergePanel.tsx), [MergeSteps.tsx](../../web/src/components/CommandTable/MergeSteps.tsx), [RecentMerges.tsx](../../web/src/components/CommandTable/RecentMerges.tsx); [web/src/lib/merge.ts](../../web/src/lib/merge.ts) (`mergeLabel`, `mergeBlock`); `useMerge`, `useRetryTransition`, `useTicketTransitions` in [queries.ts](../../web/src/api/queries.ts).
- [server/internal/fake](../../server/internal/fake) (e2e build tag): node query, REST merge and branch delete, Jira transition POST.

## Key flows

1. The Merge button of a ready row opens the panel under the row. The panel remembers the head SHA of the row and makes an `Idempotency-Key` with `crypto.randomUUID`.
2. Every linked ticket is listed with "Don't move". "Move..." loads that ticket's transitions, then a dropdown chooses one. Required fields of the transition render inline (a list for fields with allowed values, text otherwise). Each moved ticket needs its confirm checkbox. With no ticket moved the button reads "Merge without ticket" and needs its own confirmation.
3. The button reads "Merge & move to <status>" for one moved ticket, "Merge & update N tickets" for several. It is disabled, with the reason beside it, while a row block (`blockOf`), a missing required field or a missing confirmation applies.
4. `Merge` runs the steps of spec 7.7: an `Idempotency-Key` that was already used returns the stored result (a key used for another PR is `idempotency_key_reused`; two requests with the same key at once share one run). Then one GraphQL node query and the checks (open, not a draft, stack base not open, head equals `expectedHeadSha`, mergeable, clean or has hooks, no review missing), the repo allows the method, every requested transition is in the ticket's current list with its required fields.
5. `PUT /repos/{o}/{r}/pulls/{n}/merge` with `merge_method` and `sha`. 409 is `head_moved`, 405 is `not_mergeable`; both carry the fresh state and start a refresh.
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

## Tests

- [server/internal/merge](../../server/internal/merge): `flow_test.go` (every refusal, partial failure, retry, idempotent replay, concurrent same key, parallelism of 4, branch rules), `calls_test.go` (outbound call counts and one `RecordMerge`), `resolve_test.go`.
- [server/internal/github/pr_test.go](../../server/internal/github/pr_test.go), [server/internal/jira/jira_test.go](../../server/internal/jira/jira_test.go), [server/internal/store/merges_test.go](../../server/internal/store/merges_test.go), [server/internal/api/merge_api_test.go](../../server/internal/api/merge_api_test.go).
- [MergePanel.test.tsx](../../web/src/components/CommandTable/MergePanel.test.tsx), [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx).
- [e2e/merge.spec.ts](../../e2e/merge.spec.ts): merge and move with a required field, a failed move that is retried, a head that moved.
