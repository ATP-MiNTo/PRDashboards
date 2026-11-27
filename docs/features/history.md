# History view

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [merge-and-transition.md](merge-and-transition.md), [header.md](header.md), [command-table.md](command-table.md), [search.md](search.md).

## Purpose

The "Merged pull requests" view of the design. The history circle in the header swaps the stat strip, the toolbar and the list for one glass surface that lists the pull requests merged from this dashboard, newest first: avatar, title (a link to GitHub), "repo #number · author", what became of each Jira ticket ("INP-104 moved to Done", "INP-105 left in In Review"), the size of the change (+added, -removed) and a "Merged 5m ago" badge. It replaces the earlier "Recent merges" section and the per-step list under it, so there is one place for the outcome of a merge. This feature is not in the PRD or the spec.

Limits, on purpose: the app has no stored list of merged pull requests (the poller only fetches open ones), and nothing may be invented. The view lists the merges made from this tab since the page was loaded (at most 50), from the result the server returned for each merge. A reload empties it. The server's activity log keeps the full record of the Jira moves ([status-sync-rules.md](status-sync-rules.md)).

## Data model

No server data of its own and no browser storage. The TanStack Query cache entry `merges` (`RecentMerge {prId, pr, result, at}` in [web/src/api/queries.ts](../../web/src/api/queries.ts)) is written by `useMerge` when the server accepts a merge: `pr` is the pull request as the card showed it (the snapshot drops it soon after), `result` is the `MergeResult` ([merge-and-transition.md](merge-and-transition.md)) and `at` is the time the answer came. `useRetryTransition` replaces a failed step in it when the retry works. `HistoryEntry` and `HistoryTicket` in [web/src/lib/history.ts](../../web/src/lib/history.ts) are what the view draws.

## Code map

- [web/src/lib/history.ts](../../web/src/lib/history.ts): `historyEntry` (the entry of a merge), `ticketText`, `matchesHistory`, `mergedWhen` ("just now", "5m ago", "2h ago", "Yesterday", "3d ago") and `historyUrl` (the pull request link, rebuilt from validated parts).
- [web/src/components/History/HistoryView.tsx](../../web/src/components/History/HistoryView.tsx) and [History.module.css](../../web/src/components/History/History.module.css): the surface, `section` named "Merged pull requests".
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx): the `history` state, the swap and the title. [Header.tsx](../../web/src/components/Header/Header.tsx) and [HistoryButton.tsx](../../web/src/components/Header/HistoryButton.tsx): the circle ([header.md](header.md)). [web/src/theme/transition.ts](../../web/src/theme/transition.ts): `crossfade`.

## Key flows

1. The history circle toggles the view. The change runs through `crossfade` (a 0.9s cross-fade of the page with the View Transitions API, the same as the light and dark switch) with `flushSync`, so the new state is what the browser captures. With reduced motion or without View Transitions it swaps at once. The title bar reads "PRDashboards · History" while it is open. The sidebar, Refresh and the theme circle keep working.
2. Each row: a ticket badge says "KEY moved to X" (the step succeeded), "KEY left in X" (no step, or the ticket was skipped, with the status it had), "KEY did not move" (red, with a Retry button when the failed move has a pending retry), or "KEY will move to X" (GitHub only queued the merge). The ticket badge is a link to Jira when the snapshot had its URL. "No Jira link" when the pull request had none. "Branch not deleted" when the delete-branch step failed, and the server's message of every failed step is shown under the repository line. A merge GitHub only queued says "Queued <when>" in warning colours instead of "Merged".
3. Retry calls `POST /api/retries/{id}` (`useRetryTransition`), replaces the step in the cache, and refreshes the snapshot, the retries and the activity.
4. The header search filters the view: every word must be in the title, the repository, the number or a ticket key (`matchesHistory`). The count badge shows how many rows match. An empty history says "Nothing merged here yet. Pull requests you merge from this dashboard are listed until the page is reloaded."; a search that matches nothing says "Nothing merged matches the search."
5. A merge the server accepts is in the view at once, while its card still plays the launch in the list ([merge-and-transition.md](merge-and-transition.md)); the toast sums the steps up ("#490 merged, INP-104 moved to Done") and, when a step failed, says "Retry it from History". The design's wording "moved to History" is not used because the toast already says what happened.

## Outside calls

None from the view. Retry posts to the server's own API.

## Rules and invariants

- Links are built from validated parts: `githubPrUrl` for the pull request and `safeLink(url, 'jira')` for Jira (https on `*.atlassian.net` only, no credentials). No `dangerouslySetInnerHTML`, no `style` attribute.
- The 0.9s fade is off under `prefers-reduced-motion`. The clock hand animation is in [header.md](header.md).
- One source of truth: the old `RecentMerges` and `MergeSteps` components are gone.

## Tests

- [history.test.ts](../../web/src/lib/history.test.ts): entries from steps (moved, failed with a retry, left, skipped, queued, a ticket the snapshot did not have), problems, the search match, the "when" words, the link.
- [HistoryView.test.tsx](../../web/src/components/History/HistoryView.test.tsx): empty state, a merge row, the search, a queued merge with a failed move and Retry, no Jira link.
- [CommandTable.test.tsx](../../web/src/components/CommandTable/CommandTable.test.tsx): the swap, the title and `aria-pressed`.
- [e2e/merge.spec.ts](../../e2e/merge.spec.ts): the merge flows read the outcome in the History view, including a failed move that is retried.
