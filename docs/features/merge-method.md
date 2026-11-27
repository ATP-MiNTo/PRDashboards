# Merge method

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 7.6. Related: [settings.md](settings.md), [command-table.md](command-table.md), [merge-and-transition.md](merge-and-transition.md), [poller.md](poller.md).

## Purpose

Decides how each pull request is merged (squash, merge commit or rebase) from the repo's ordered
branch rules, the repo default and what the repo allows, shows the result in the merge dialog (the
"Method" line, with where it comes from) and lets the person change it for one merge. Repo defaults and branch rules are edited in the
repository dialog of the [Settings sidebar](settings.md).

Differences from the spec:

- The warning text is "Repo disallows squash merge, using merge commit" (the names of the configured and the chosen method). When the repo allows no method at all it is "Repo allows no merge method" and the pull request keeps the configured method.
- A branch pattern is an exact name or a glob where `*` matches any run of characters including `/`. Only letters, digits and `. _ / * -` are accepted, 1 to 200 characters.

## Data model

- `repos.default_method`, `repos.method_inherit` (1: the repository follows the global default method, which a new repository does until its dialog says otherwise), `repos.delete_branch` (`default`, `yes`, `no`), `repos.is_new` and `repo_branch_rules(repo_id, position, pattern, method)` ([storage.md](storage.md)). A new repo is seeded with the rules `main` to merge commit and `production` to merge commit (`store.SeedRules`).
- `Settings.defaultMethodForNewRepos` is the default of a repo seen for the first time.
- API schemas: `MergeMethodName`, `MethodResolution {method, default, source, allowed, warning}` (the `mergeMethod` field of every `SnapshotPr`), `BranchRule`, `RepoSettings`, `Repos`, `RepoUpdate {defaultMethod, inheritMethod?, deleteBranch?, branchRules (at most 50), clearNew, approvalsRequired?}`; `RepoSettings` also has `inheritMethod` and `deleteBranch` (`DeleteBranchChoice`). An update replaces `inheritMethod` every time (left out means false), while one that leaves `deleteBranch` out keeps it. `approvalsRequired` (0 to 10, default 0) is the repo's approvals setting, documented in [review-actions.md](review-actions.md); an update that leaves it out keeps the stored value.
- `source` is `rule: <pattern>`, `repo default` or `new repo default`.

## Code map

- [server/internal/merge/resolve.go](../../server/internal/merge/resolve.go): pure `Resolve`, `Match`, `Allowed`, `Permits`. No network, database or clock.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): `mergeRepos`, `newRepo` and `resolutionOf` fill `mergeMethod` for every row from the stored repo configuration and the repo's allowed methods.
- [server/internal/store/repos.go](../../server/internal/store/repos.go): `Repos`, `Repo`, `PutRepo` (one transaction replaces the rule list).
- [server/internal/api/handlers_repos.go](../../server/internal/api/handlers_repos.go): `ListRepos`, `GetRepo`, `PutRepo`, which asks the poller to rebuild the snapshot from cached data.
- [web/src/lib/merge.ts](../../web/src/lib/merge.ts): `METHOD_ORDER`, `METHOD_NAMES`, `effectiveMethod`, `methodFrom` (the source in words) and `methodWord`. The card has no method column any more: [MergeDialog.tsx](../../web/src/components/CommandTable/MergeDialog.tsx) shows the method and its source and holds the per-merge choice, in a [PopMenu](../../web/src/components/PopMenu/PopMenu.tsx).

## Key flows

1. Resolution: the first branch rule whose pattern matches the PR's base branch wins, else the repo default (the global default for a repo not stored yet, and for a repo with `inheritMethod`: `mergeRepos` in the poller puts the global method into `merge.Repo`, with `Follows` set so the source says "new repo default"). If the repo disallows that method, the first allowed method in the order merge commit, squash, rebase is used and the warning is set.
2. Every refresh and every `PUT /api/repos/{id}` rebuilds the snapshot, so `mergeMethod` follows the configuration without an outside call.
3. The merge dialog's first bullet reads "Method: [merge commit], from branch rule main" (`methodFrom`: "branch rule <pattern>", "the repo settings" or "the defaults"), with the warning under it. The method is a small dropdown button: "Squash", "Merge commit" and "Rebase", the resolved one marked "(default)" and the disallowed ones "(not allowed)" and disabled. When another method is chosen the line says ", chosen for this merge".
4. Choosing another method in the dialog changes that merge only. It is dialog state, dropped when the dialog closes, and `effectiveMethod` falls back to the resolved method when the repo stops allowing the choice. The method sent to the server is always the shown one.
5. Changing `defaultMethodForNewRepos` in `PUT /api/settings` calls `Poller.Relink` when it differs from the stored one, so repositories that follow it resolve with the new method at once.
6. `PUT /api/repos/{id}` validates the body against the schema (pattern shape, at most 50 rules), replaces the repo's rules and default in one transaction, optionally clears `is_new`, then calls `Poller.Relink`.

## Outside calls

None. The allowed methods come from the repository fields of the search query ([poller.md](poller.md)).

## Rules and invariants

- `merge.Resolve` stays pure and is covered at 90 % or more (`pnpm coverage-check`).
- The server checks again, before merging, that the repo allows the requested method (`method_not_allowed`, [merge-and-transition.md](merge-and-transition.md)).
- The rule list is replaced as a whole, in one transaction, never row by row.
- A repo the store has not seen is resolved with the global default and the seeded rules, so the first snapshot is already right.

## Tests

- [server/internal/merge/resolve_test.go](../../server/internal/merge/resolve_test.go): table tests of rule order, glob matching, defaults, fallback order and warnings.
- [server/internal/store/repos_test.go](../../server/internal/store/repos_test.go), [server/internal/api/merge_api_test.go](../../server/internal/api/merge_api_test.go) (repo settings validation, 404, rebuild on save), [server/internal/poller/merge_method_test.go](../../server/internal/poller/merge_method_test.go) (`TestARepoThatFollowsTheDefaultTakesTheGlobalMethod`). Inheritance and the delete-branch choice: `TestRepoPrefsNewReposFollowTheDefaultAndUpdatesReplaceOrKeepThem` in the store tests, `TestRepoMethodInheritanceAndDeleteBranchChoice` and `TestChangingTheDefaultMethodResolvesThePullRequestsAgain` in the API tests.
- [merge.test.ts](../../web/src/lib/merge.test.ts) (`methodFrom`, `methodWord`, `effectiveMethod`), [MergeDialog.test.tsx](../../web/src/components/CommandTable/MergeDialog.test.tsx) (the resolved method, its source and warning, a per-merge choice that is sent, disallowed methods), [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx), [e2e/merge.spec.ts](../../e2e/merge.spec.ts) ("merge commit" from "branch rule main").
