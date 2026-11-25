# Stacked PRs

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 7.2. Related: [command-table.md](command-table.md), [poller.md](poller.md).

## Purpose

Detects pull requests that are stacked on each other and shows them as grouped rows with a
connector line, so the order of merging is clear. The "Merge after #N" text is shown in the browser
and the server refuses to merge a child while its base pull request is open
([merge-and-transition.md](merge-and-transition.md)).

## Data model

`SnapshotPr.stack` is `null` or `StackInfo {id, position, size, parentId, parentNumber}`. `id` is the id of the PR at the base of the stack. Nothing is stored in SQLite.

## Code map

- [server/internal/stacks/stacks.go](../../server/internal/stacks/stacks.go): pure `Detect` and `Info.MergeBlock`.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): runs `Detect` and fills `stack`.
- [server/internal/merge/flow.go](../../server/internal/merge/flow.go): `check` refuses a child whose base is still open.
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `groupsOf` keeps a stack together. [Table.tsx](../../web/src/components/CommandTable/Table.tsx) and [Row.tsx](../../web/src/components/CommandTable/Row.tsx) draw the stack header and connector.

## Key flows

1. `build` passes every open PR (`repo`, `headRefName`, `baseRefName`) to `Detect`.
2. `Detect` indexes PRs by `repo|headRefName` in one pass. A PR whose base branch is the head branch of another PR in the same repo is its child. Each stack gets positions from the base up.
3. The UI groups the rows of one stack under a header and shows "Merge after #N" in the Merge column of every child.

4. Merging: GitHub-native stacks merge only through GitHub's asynchronous merge API, because the synchronous merge answers 403 for a stacked pull request. The merge flow handles that on its own ([merge-and-transition.md](merge-and-transition.md), "Asynchronous merge"); it is not a permission error and needs no action. GitHub merges every unmerged pull request below the one merged, atomically, and rebases the pull request above onto the base. The dashboard still offers Merge only on the bottom open pull request of a stack (`stack_base_open` for the others). The head branch of the merged pull request is kept while the pull request above uses it as its base.

## Outside calls

None. Detection uses the snapshot only.

## Rules and invariants

- O(n): one pass with a map, no nested scans.
- Cycles are ignored and, when several PRs share a head branch, the first one is the parent.
- The package is pure: no network, database or clock.
- Enforced by the merge: `POST /api/prs/{id}/merge` answers 409 `stack_base_open` when another open PR has the pull request's base branch as its head (`merge.check`, using `Snapshot.Refs` of the repo).

## Tests

- [server/internal/stacks/stacks_test.go](../../server/internal/stacks/stacks_test.go): table tests, 100 % coverage.
- [server/internal/merge/flow_test.go](../../server/internal/merge/flow_test.go): the `stack_base_open` refusal. [server/internal/github/merge_async_test.go](../../server/internal/github/merge_async_test.go): the stack refusal goes to the async merge.
- [server/internal/poller/poller_test.go](../../server/internal/poller/poller_test.go): stack info in the snapshot.
- [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts) (`groupsOf`) and [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx).
