# Merge method

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 7.6. Related: [settings.md](settings.md), [command-table.md](command-table.md), [merge-and-transition.md](merge-and-transition.md), [poller.md](poller.md).

## Purpose

Decides how each pull request is merged (squash, merge commit or rebase) from the repo's ordered
branch rules, the repo default and what the repo allows, shows the result in the Method column and
lets the person change it for one merge. Repo defaults and branch rules are edited in the
[settings drawer](settings.md).

Differences from the spec:

- The warning text is "Repo disallows squash merge, using merge commit" (the names of the configured and the chosen method). When the repo allows no method at all it is "Repo allows no merge method" and the pull request keeps the configured method.
- A branch pattern is an exact name or a glob where `*` matches any run of characters including `/`. Only letters, digits and `. _ / * -` are accepted, 1 to 200 characters.

## Data model

- `repos.default_method`, `repos.is_new` and `repo_branch_rules(repo_id, position, pattern, method)` ([storage.md](storage.md)). A new repo is seeded with the rules `main` to merge commit and `production` to merge commit (`store.SeedRules`).
- `Settings.defaultMethodForNewRepos` is the default of a repo seen for the first time.
- API schemas: `MergeMethodName`, `MethodResolution {method, default, source, allowed, warning}` (the `mergeMethod` field of every `SnapshotPr`), `BranchRule`, `RepoSettings`, `Repos`, `RepoUpdate {defaultMethod, branchRules (at most 50), clearNew}`.
- `source` is `rule: <pattern>`, `repo default` or `new repo default`.

## Code map

- [server/internal/merge/resolve.go](../../server/internal/merge/resolve.go): pure `Resolve`, `Match`, `Allowed`, `Permits`. No network, database or clock.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): `mergeRepos`, `newRepo` and `resolutionOf` fill `mergeMethod` for every row from the stored repo configuration and the repo's allowed methods.
- [server/internal/store/repos.go](../../server/internal/store/repos.go): `Repos`, `Repo`, `PutRepo` (one transaction replaces the rule list).
- [server/internal/api/handlers_repos.go](../../server/internal/api/handlers_repos.go): `ListRepos`, `GetRepo`, `PutRepo`, which asks the poller to rebuild the snapshot from cached data.
- [web/src/lib/merge.ts](../../web/src/lib/merge.ts): `methodOptions`, `effectiveMethod`, `sourceHint`. [MethodCell.tsx](../../web/src/components/CommandTable/MethodCell.tsx) and [Dropdown.tsx](../../web/src/components/Dropdown/Dropdown.tsx): the Method column.

## Key flows

1. Resolution: the first branch rule whose pattern matches the PR's base branch wins, else the repo default (the global default for a repo not stored yet). If the repo disallows that method, the first allowed method in the order merge commit, squash, rebase is used and the warning is set.
2. Every refresh and every `PUT /api/repos/{id}` rebuilds the snapshot, so `mergeMethod` follows the configuration without an outside call.
3. The Method column shows the resolved method as "<Method> (default)", the other allowed methods, and the disallowed ones as "<Method> (not allowed)" and disabled. Under it the source ("Branch rule main", "Repo default", "New repo default") and the warning.
4. Choosing another method in the column changes that row's next merge only. It is component state and is dropped when the repo stops allowing it.
5. `PUT /api/repos/{id}` validates the body against the schema (pattern shape, at most 50 rules), replaces the repo's rules and default in one transaction, optionally clears `is_new`, then calls `Poller.Relink`.

## Outside calls

None. The allowed methods come from the repository fields of the search query ([poller.md](poller.md)).

## Rules and invariants

- `merge.Resolve` stays pure and is covered at 90 % or more (`pnpm coverage-check`).
- The server checks again, before merging, that the repo allows the requested method (`method_not_allowed`, [merge-and-transition.md](merge-and-transition.md)).
- The rule list is replaced as a whole, in one transaction, never row by row.
- A repo the store has not seen is resolved with the global default and the seeded rules, so the first snapshot is already right.

## Tests

- [server/internal/merge/resolve_test.go](../../server/internal/merge/resolve_test.go): table tests of rule order, glob matching, defaults, fallback order and warnings.
- [server/internal/store/repos_test.go](../../server/internal/store/repos_test.go), [server/internal/api/merge_api_test.go](../../server/internal/api/merge_api_test.go) (repo settings validation, 404, rebuild on save), [server/internal/poller/merge_method_test.go](../../server/internal/poller/merge_method_test.go).
- [Dropdown.test.tsx](../../web/src/components/Dropdown/Dropdown.test.tsx), [MethodCell.test.tsx](../../web/src/components/CommandTable/MethodCell.test.tsx).
