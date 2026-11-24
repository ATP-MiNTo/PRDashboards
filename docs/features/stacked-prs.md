# Stacked PRs

Status: Partial. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 7.2. Related: [command-table.md](command-table.md), [poller.md](poller.md).

## Purpose

Detects pull requests that are stacked on each other and shows them as grouped rows with a
connector line, so the order of merging is clear. Partial: the "Merge after #N" text is shown, but
the server does not yet refuse a merge of a child, because merging arrives in milestone 6.

## Data model

`SnapshotPr.stack` is `null` or `StackInfo {id, position, size, parentId, parentNumber}`. `id` is the id of the PR at the base of the stack. Nothing is stored in SQLite.

## Code map

- [server/internal/stacks/stacks.go](../../server/internal/stacks/stacks.go): pure `Detect` and `Info.MergeBlock`.
- [server/internal/poller/build.go](../../server/internal/poller/build.go): runs `Detect` and fills `stack`.
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `groupsOf` keeps a stack together. [Table.tsx](../../web/src/components/CommandTable/Table.tsx) and [Row.tsx](../../web/src/components/CommandTable/Row.tsx) draw the stack header and connector.

## Key flows

1. `build` passes every open PR (`repo`, `headRefName`, `baseRefName`) to `Detect`.
2. `Detect` indexes PRs by `repo|headRefName` in one pass. A PR whose base branch is the head branch of another PR in the same repo is its child. Each stack gets positions from the base up.
3. The UI groups the rows of one stack under a header and shows "Merge after #N" in the Merge column of every child.

## Outside calls

None. Detection uses the snapshot only.

## Rules and invariants

- O(n): one pass with a map, no nested scans.
- Cycles are ignored and, when several PRs share a head branch, the first one is the parent.
- The package is pure: no network, database or clock.
- Not yet enforced: the server must refuse to merge a child before its base is merged (milestone 6).

## Tests

- [server/internal/stacks/stacks_test.go](../../server/internal/stacks/stacks_test.go): table tests, 100 % coverage.
- [server/internal/poller/poller_test.go](../../server/internal/poller/poller_test.go): stack info in the snapshot.
- [web/src/lib/snapshot.test.ts](../../web/src/lib/snapshot.test.ts) (`groupsOf`) and [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx).
