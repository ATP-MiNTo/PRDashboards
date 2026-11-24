# Status sync rules and Multi-PR tickets

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 7.3, 7.4, 7.5 and 7.7. Related: [poller.md](poller.md), [merge-and-transition.md](merge-and-transition.md), [settings.md](settings.md), [ticket-linking.md](ticket-linking.md), [storage.md](storage.md).

## Purpose

One ordered list of rules keeps Jira tickets in the right status during the life of their pull
requests, including at merge. A rule is "when `<trigger>` and `<filters>` then move the ticket to
`<status>`" with a mode, All or Any, for tickets that have several pull requests. A rule fires once
per change, so a manual move in Jira is not undone until something new happens.

Differences from the spec:

- Merged pull requests count for `merged` rules and for the set of a mode All rule only inside a 30 day window (`mergedWindow`). A merged pull request is ignored by every other trigger, and a closed one never counts.
- `ready_for_review` uses `prs.ready_sha`, the head SHA when the PR last became ready. A push keeps it, going back to draft clears it.
- A rule whose target status has no transition from the ticket's current status is recorded once as `skipped` and not retried. A failed lookup of the transitions records nothing, so the next refresh retries.
- A transition with required fields is not sent. It is recorded as `needs_input` and shown in the Activity list, because nobody is there to fill the fields.
- Changing a rule's mode saves with the baseline, so nothing moves because of the change.
- Preview and baseline use the cached snapshot data and make no outside call.

## Data model

- `sync_rules` (`id`, `position`, `name`, `enabled`, `trigger`, `filters` JSON, `target_status`, `mode`) and `rule_firings` (`rule_id`, `ticket_key`, `fingerprint`, `fired_at`, deleted with the rule) ([storage.md](storage.md)). Migration `0003_sync.sql` adds `prs.ready_sha`.
- API schemas in [api/openapi.yaml](../../api/openapi.yaml): `SyncRule`, `RuleTrigger`, `RuleMode`, `RuleFilters`, `Rules`, `RulesUpdate` (`rules`, `baseline`), `RulePreviewRequest`, `RulePreview`, `RuleMove`, `RuleWaiting`, `PrRef`, `TicketWaiting` and `TicketMergePlan`. `SnapshotTicket` has `waiting` and `mergePlan`, both nullable.
- Activity rows of a rule have `triggered_by = "rule:<id>"` and the result `ok`, `failed`, `skipped` or `needs_input`. A failed move adds a `pending_retries` row without a PR.
- Limits: at most 100 rules, `commit_stale` hours 1 to 720, names, statuses and issue types up to 100 characters, repo and branch globs up to 200 characters of letters, digits and `. _ / * -`, a Jira project of letters, digits and `_`. Triggers and modes are strict enums.

## Code map

- [server/internal/rules/rules.go](../../server/internal/rules/rules.go): pure engine. `Evaluate` returns `Firing`s, `Waiting` entries and `Hold`s. `Match` is the glob, `PreviewRule`, `Validate`, `MergePlan` and `AfterMerge`.
- [server/internal/store/syncrules.go](../../server/internal/store/syncrules.go): `Rules`, `PutRules` (whole list, one transaction, firings reset for changed rules, optional baseline), `SaveSync`, `SyncState`. [migrations/0003_sync.sql](../../server/internal/store/migrations/0003_sync.sql). [refresh.go](../../server/internal/store/refresh.go) writes the sync result inside `SaveRefresh`.
- [server/internal/poller/sync.go](../../server/internal/poller/sync.go): `runRules`, `fire` (at most 4 in parallel), `move`, `Reevaluate`, `Preview`, `Baseline`, `MergedFirings`. [refresh.go](../../server/internal/poller/refresh.go) calls it before `build`, [build.go](../../server/internal/poller/build.go) puts `waiting` and `mergePlan` on tickets.
- [server/internal/api/handlers_rules.go](../../server/internal/api/handlers_rules.go): `GetRules`, `PutRules`, `PreviewRule`.
- [server/internal/merge/flow.go](../../server/internal/merge/flow.go): records the `merged` fingerprints of a merge through `MergedFirings`.
- Web: [web/src/lib/rules.ts](../../web/src/lib/rules.ts) (labels, `ruleProblem`, `waitingText`, `MULTI_PR_HINT`), [RulesSection.tsx](../../web/src/components/Settings/RulesSection.tsx), [RuleForm.tsx](../../web/src/components/Settings/RuleForm.tsx), [MultiPrSection.tsx](../../web/src/components/Settings/MultiPrSection.tsx), [TicketCell.tsx](../../web/src/components/CommandTable/TicketCell.tsx), [MergePanel.tsx](../../web/src/components/CommandTable/MergePanel.tsx), [ActivitySection.tsx](../../web/src/components/Settings/ActivitySection.tsx), and `useRules`, `useSaveRules`, `usePreviewRule` in [queries.ts](../../web/src/api/queries.ts).
- Fakes for e2e: `multiPr` and `approved` in [server/internal/fake/github.go](../../server/internal/fake/github.go), INP-105 and transition 41 "Ready to Merge" in [jira.go](../../server/internal/fake/jira.go).

## Key flows

1. After each refresh, `runRules` reads the stored firings (`SyncState`), builds one `rules.Input` from the open pull requests, the merged ones of the last 30 days and the fetched tickets, and calls `rules.Evaluate`. The rules are walked in order, the first enabled rule that holds and whose fingerprint differs from the stored one fires, at most one per ticket.
2. Each trigger has its fingerprint from the spec table. Mode All fingerprints the whole set, `sorted(prID=fingerprint)` joined by commas, so it fires once, when the last pull request meets the rule. Mode Any fires for the first pull request that meets it. Unavailable data (no CI result, no head SHA, Jira data missing) never matches.
3. A ticket already in the target status records the firing and sends nothing. Otherwise `fire` fetches the transitions of that ticket (60 s cache), picks the one whose `to` name equals the target ignoring case, and posts it. At most 4 tickets run in parallel.
4. The activity rows, the `rule_firings` and the retries of one refresh are written in the same transaction as the snapshot (`SaveRefresh`). Then the `activity` and `snapshot` events are published.
5. Saving the list (`PUT /api/rules` with `{rules, baseline}`) replaces it in one transaction. A rule is changed when it is new or its enabled flag, trigger, hours, filters, target or mode differ (the name does not count). Changed rules lose their firings. With `baseline: true` the current holds of the changed rules are stored in the same transaction, so existing tickets do not move. Then `Reevaluate` runs from cached data and moves tickets whose rule now fires.
6. `POST /api/rules/preview` evaluates one rule against the cached data and answers the tickets it would move now, the ones already in the target status and the ones waiting. The form needs a preview of the rule as it stands before Save is possible, and offers "Move them" or "Don't move existing tickets" when something would move.
7. Merge panel: `mergePlan` (`rules.AfterMerge`) says what the first matching `merged` rule does once this pull request is merged. The panel preselects that transition when it is loaded, and the person still confirms it. When other pull requests must merge first (mode All) it shows "Won't move yet: waiting on ..." and defaults to "Don't move". The dropdown can still move the ticket. The merge flow records the `merged` fingerprints with the just merged pull request counted as merged, so the engine does not fire again.
8. Multi-PR tickets section: one chip per rule, all new rules start at All. Click or Enter sets All, right-click or Shift+Enter sets Any, each saves at once with `baseline: true`.
9. A ticket whose All rule waits (some pull requests meet it, others do not, and the ticket is not in the target status) shows "Waiting on 2 of 3 PRs: FE #34, proxy #56" in the ticket cell.

## Outside calls

- Evaluating, previewing and the baseline make none.
- A firing ticket that is not yet in the target status costs one `GET /rest/api/3/issue/{key}/transitions?expand=transitions.fields` (cached 60 s) and, when a transition matches without required fields, one `POST` to move it. Tickets that do not fire and tickets already in the target status cost nothing.
- Opening the merge panel for a ticket with a merge plan that is not waiting fetches its transitions (cached).

## Rules and invariants

- `internal/rules` is pure: no network, database or clock (the time is an input).
- A rule fires only when the fingerprint differs from the stored one, so restarts catch up on what happened while the app was closed and a manual move is not undone.
- At most one transition per ticket per evaluation, and the first matching enabled rule wins.
- Every move re-checks the transitions from Jira before it is sent. A deleted rule's firings are dropped by the store (the insert joins `sync_rules`).
- The list is written whole in one transaction, no per-row loops. The browser never triggers an outside call by saving rules.
- The hint text of the Multi-PR section is exactly the sentence of TECH_SPEC 7.5, shown as a tooltip and as the accessible description of the ⓘ button.

## Tests

- [server/internal/rules/rules_test.go](../../server/internal/rules/rules_test.go): every trigger, filter, mode, fingerprint change and no change, order, one per ticket, already in target, unavailable data, closed pull requests, catch-up, preview, globs, validation and the merge plan (99% coverage, gate 90%).
- [server/internal/poller/sync_test.go](../../server/internal/poller/sync_test.go): merged rule, transitions fetched only for firing tickets, mode All after the third pull request, Any, in target, needs input, failed move and retry, missing transition, lookup failure retried, catch-up after a restart, manual move not undone, Jira unavailable, 4 in parallel, one write per refresh, preview, baseline, merged fingerprints, merge plan.
- [server/internal/store/syncrules_test.go](../../server/internal/store/syncrules_test.go), [server/internal/api/rules_api_test.go](../../server/internal/api/rules_api_test.go), [server/internal/merge/flow_test.go](../../server/internal/merge/flow_test.go).
- Web: [RulesSection.test.tsx](../../web/src/components/Settings/RulesSection.test.tsx), [MultiPrSection.test.tsx](../../web/src/components/Settings/MultiPrSection.test.tsx), [TicketCell.test.tsx](../../web/src/components/CommandTable/TicketCell.test.tsx), [MergePanel.test.tsx](../../web/src/components/CommandTable/MergePanel.test.tsx), [SettingsDrawer.test.tsx](../../web/src/components/Settings/SettingsDrawer.test.tsx).
- [e2e/multipr.spec.ts](../../e2e/multipr.spec.ts): the rule is created in the drawer, three approvals through the real poller, the ticket moves only after the third, with one transitions lookup and one move.
