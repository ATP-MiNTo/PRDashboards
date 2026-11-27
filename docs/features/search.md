# Search

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [header.md](header.md), [command-table.md](command-table.md), [toolbar-pager-footer.md](toolbar-pager-footer.md).

## Purpose

Finds pull requests in the list by what is typed: title, repository, PR number or Jira key. It runs in the browser over the snapshot the table already has. The PRD and spec do not describe it; it is part of the v2 UI migration.

## Data model

No server data and no browser storage. The text is component state of `useListPrefs` (`query`), so a reload starts with an empty search.

## Code map

- [web/src/lib/search.ts](../../web/src/lib/search.ts): `searchTerms(query)` and `matchesSearch(pr, terms)`. Pure.
- [web/src/lib/snapshot.ts](../../web/src/lib/snapshot.ts): `groupsOf` takes the terms and drops the pull requests that do not match, before grouping and stacks.
- [web/src/components/Toolbar/useListPrefs.ts](../../web/src/components/Toolbar/useListPrefs.ts): `query`, the memoized `terms` and `setQuery`, which sets the page to 1.
- [web/src/components/Header/SearchBox.tsx](../../web/src/components/Header/SearchBox.tsx): the field, in two variants. `bar` sits in the right header pill from 768px up (14rem wide, placeholder "Title, repo, PR or Jira key"). `page` is the full-width field above the stat strip below 768px (placeholder "Search pull requests"). Both are named "Search pull requests".
- [web/src/components/CommandTable/List.tsx](../../web/src/components/CommandTable/List.tsx): passes the terms and shows the "Nothing here" message when a search leaves nothing.

## Key flows

1. Typing calls `setQuery`. The page goes back to 1 and `terms` is recomputed.
2. `searchTerms` lowercases the text, splits it on white space and drops a leading `#` from a number ("#101" finds PR 101).
3. `matchesSearch` keeps a pull request when every term is found in its title, `repo.nameWithOwner`, number or any linked ticket key. Terms may match different fields ("web login" finds "Add login" in acme/web).
4. `groupsOf` applies the stat tile filter, the Show mode and the search together; a stack keeps the members that match.

## Outside calls

None.

## Rules and invariants

- Pure logic in `lib/search.ts`: no network, no storage, no clock.
- The search never changes the snapshot or the counts of the Show and Hidden PRs switches; it only narrows the list. The stat tiles count what the search leaves.
- The two fields share one state, so the text typed in one shows in the other. Only one is visible at a time (a media query).
- An empty search lists everything. Branch names and review state are not searched.

## Tests

- [web/src/lib/search.test.ts](../../web/src/lib/search.test.ts): terms, each field, `#` numbers, several terms, branches not searched, order kept, combination with the filter, empty search.
- [Header.test.tsx](../../web/src/components/Header/Header.test.tsx) and [ListControls.test.tsx](../../web/src/components/CommandTable/ListControls.test.tsx): typing filters the table, resets the page, the empty message.
