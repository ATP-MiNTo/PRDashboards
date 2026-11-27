# Toolbar, show by role, hidden pull requests, pager and footer

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) section 11. Related: [command-table.md](command-table.md), [poller.md](poller.md), [sidebar-shell.md](sidebar-shell.md), [stacked-prs.md](stacked-prs.md).

## Purpose

How the pull request list is viewed, all in the browser over the snapshot the table already has: sorting, showing by role, hiding pull requests you do not want to see, paging, and the page footer with the GitHub button. Part of the v2 UI migration; the PRD and spec do not describe these controls.

- The search box is in the header ([search.md](search.md)); on narrow screens a full-width field sits above the stat strip.
- The toolbar sits under the stat strip ([command-table.md](command-table.md); the strip's tiles are the filters, the status chips are gone): "Sort", a select, a square button that reverses the direction, a divider, the "Show" switch, the "Hidden PRs" switch and, at the right end, a legend. The row wraps on narrow screens (the divider is hidden there).
- Each card has a "Hide" checkbox. The "Hidden PRs" switch picks what the list shows.
- The legend lists the cues of the cards: "Assigned" and "Opened by me" (the same green dot, `--rc-a`), "Review requested" (the red dot, `--rc-r`) and "Stack", each with a coloured dot.
- The pager is under the list: "Showing X to Y of N" on the left, the page size select and the page buttons on the right.
- The footer ends the page.

## Data model

No server data and no new API. Browser storage, all in `localStorage`, every read and write in `try/catch` (blocked storage means defaults and choices that last for the visit):

| Key | Value | Written when |
| --- | --- | --- |
| `prd-hidden` | JSON array of PR ids | a Hide box is toggled |
| `prd-show` | `all`, `mine` or `review` (the values of the earlier switch map on read: `everything` to `all`, `assigned` and `authored` to `mine`) | the Show switch changes |
| `prd-hidef` | `visible`, `all` or `hidden` | the switch changes |
| `prd-pagesize` | `10`, `15` or `20` | the page size changes |

Anything else stored under these keys is ignored (for `prd-show` it reads as `all`). Sort, direction, filter (a stat tile) and page are component state and start as "Recently updated", natural direction, "All" (the Showing tile) and page 1.

Hidden ids that are no longer in the snapshot are ignored when read (`pruneHidden`), so the Hidden count and the lists never include them. Nothing is written while rendering; the stale ids leave storage with the next toggle, which writes the pruned set. While the snapshot has no pull requests (still loading) nothing is pruned.

## Code map

- [web/src/lib/sort.ts](../../web/src/lib/sort.ts): `SORT_OPTIONS`, `sortGroups` and `sizeOf`. Pure.
- [web/src/lib/show.ts](../../web/src/lib/show.ts): `ShowMode` (`all`, `mine`, `review`), `parseShowMode`, `rolesOf`, `roleKind`, `inShow`, `showCounts`. Pure. `groupsOf` in [snapshot.ts](../../web/src/lib/snapshot.ts) takes the mode and drops the other pull requests before grouping.
- [web/src/lib/view.ts](../../web/src/lib/view.ts): `applyHidden`, `shownIn`, `pruneHidden`, `paginate`, `pageRange`, `pageItems`, page sizes and `listStats` (the numbers of the stat tiles: showing, ready, running, blocked, draft, stale and stacks; the clock of Stale is `ListView.now`). Pure.
- [web/src/lib/storage.ts](../../web/src/lib/storage.ts): `readStored`, `writeStored`, `readStringList` (never throw) and `reducedMotion`.
- [web/src/components/Toolbar/useListPrefs.ts](../../web/src/components/Toolbar/useListPrefs.ts): the view state (filter, sort, direction, show, hidden set, mode, page size, page). Every setter that changes what is listed sets the page to 1.
- [web/src/components/Toolbar/Toolbar.tsx](../../web/src/components/Toolbar/Toolbar.tsx): the sort controls, the "Show" and "Hidden PRs" switches with their hints, and the legend.
- [web/src/components/Toolbar/RollSwitch.tsx](../../web/src/components/Toolbar/RollSwitch.tsx): the reusable rolling switch (`name`, `options`, `value`, `onChange`, `count`). Used for "Show" and "Hidden PRs", which only click through every option. The optional `defaultChoice {id, resolvesTo?, remembered?}` makes one option the "use the default" state, for the rolls of the repository dialog ([settings.md](settings.md)): a click then turns through the other options only (from the default it starts after `resolvesTo`), and a right-click, Shift+click (which is what Shift+Enter and Shift+Space send) or the Menu key switches the default off, to `remembered` else `resolvesTo`, or on. Its tooltip and accessible name then read "Click: next choice. Right-click, Shift+click or Shift+Enter: use default or choose"; without it the switch is unchanged.
- [web/src/components/Toolbar/Hint.tsx](../../web/src/components/Toolbar/Hint.tsx): a label with a popover that opens on hover and on keyboard focus.
- [web/src/components/Toolbar/Pager.tsx](../../web/src/components/Toolbar/Pager.tsx): "Showing X to Y of N", page size select and page buttons.
- [web/src/components/Toolbar/Footer.tsx](../../web/src/components/Toolbar/Footer.tsx): the footer, the GitHub button and `fabPlacement`.
- [Toolbar.module.css](../../web/src/components/Toolbar/Toolbar.module.css), [Footer.module.css](../../web/src/components/Toolbar/Footer.module.css): styles on the theme tokens. The sort select (181px wide), the page size select (127px), the roll switches (144px), the sort arrow and the page buttons are 32px controls with radius .5rem: `ui-select` and `ui-btn` from [theme.md](theme.md) (an outline button that fills with ink on hover and scales to .95 while pressed). The page buttons are a daisyUI join: 1px overlap, the ends rounded, the current page on `--btn-on-bg`. The legend dots are the role colours `--rc-a` (Assigned and Opened by me), `--rc-r` (Review requested) and `--stack`.
- [web/src/components/CommandTable/List.tsx](../../web/src/components/CommandTable/List.tsx) and [Card.tsx](../../web/src/components/CommandTable/Card.tsx): the pipeline (group, sort, hide, page), the Hide checkbox and the ghost animations. [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx) wires the hook, the toolbar and the footer.

## Key flows

1. `List` selects groups from the snapshot: `groupsOf` (filter, Show, search and stacks), `sortGroups`, `applyHidden`. `paginate` then cuts the result into pages and the current page is shown. Structural sharing keeps the selected groups equal when nothing in them changed.
2. Sort: the options are Recently updated (`updatedAt`, newest first; a snapshot without it uses the last commit time, and a missing time sorts last), Title, Repository (then PR number), Size of change (additions plus deletions, biggest first) and Ready to merge first (ready, then blocked, then draft). Title and Repository start ascending, the others descending. The arrow button flips the direction (`aria-pressed` says whether it is flipped); the arrow points up in the natural order and swings round its foot (`translateY(-14px) rotate(180deg)`, .7s) after a click. The sort by PR number of the earlier table is gone: the design has no such option. A stack is one unit: it sorts by its best member in the current direction and its members keep stack order. Equal keys keep the snapshot order.
3. Hide: the Hide checkbox ("Hide #123", a 16px `ui-check` with the text "Hide" at 12px) toggles the id in the set and saves it. If the card would leave the list in the current mode (hiding in Visible, showing in Hidden), the card first plays `ghostOut` (.48s, `GHOST_MS`) and the change is applied when it ends; the box shows its new state at once. A card that stays plays `ghostDim` (hidden in All mode) or `ghostIn` (shown again) once, .5s (`PULSE_MS`). Under `prefers-reduced-motion` the change is applied at once and nothing plays. In All mode hidden cards are at 50% opacity. In a stack, only the hidden members leave; the stack keeps the rest.
4. The "Show" switch sits before "Hidden PRs" and cycles All (default, hint "Everything"), Mine (a green dot before the label, hint "Assigned to you or opened by you") and To review (a red dot, hint "Waiting on your review") over `SnapshotPr.roles`. Mine lists a PR with the `assigned` or the `authored` role, To review one with the `reviewer` role; a PR with both kinds of role is in both modes. The dot is part of the option (`RollOption.tone`, `green` or `red`, the `rollDot` classes on `--rc-a` and `--rc-r`), drawn inside the rolling label so it rolls with it, and decoration only: the accessible name stays "Show, now Mine, 3. Activate to switch.". The badge counts the whole snapshot for the current state (ignoring the stat tile filter, the search and hidden PRs; repositories switched off are left out). The stat tiles and the Showing number follow the mode (`listStats` takes `ListView.show`), so choosing To review shows the tiles of the review requested pull requests only. The label "Show" is a Hint that explains each state and opens on hover and keyboard focus like the other one. The label of a switch rolls up .28s with the new one coming from below; a hover fills the button with ink and a press scales it to .95. The state is saved as `prd-show`, and the page goes back to the first page when it changes. A PR without roles (an older snapshot) counts as assigned and opened, so it is in All and Mine and never in To review. In a stack only the members the state lists stay in the group.
5. The "Hidden PRs" switch cycles Visible, All, Hidden. The button shows the current label and a badge with how many pull requests that state shows (ignoring the stat tile filter). The label rolls upward when it changes (off under reduced motion), and a polite live region announces "Hidden PRs: All". The label "Hidden PRs" opens a hint with the three states on hover and on focus (it is focusable), has `aria-describedby` pointing at the popover (`role="tooltip"`) while it is open, and closes on Escape, blur or when the pointer leaves.
6. Paging: a page is closed once it holds the page size or more pull requests, so a stack is never split and a page can run over its size. The pager renders whenever there is at least one pull request (so the size can be changed back); the page buttons (previous, numbers with gaps for long lists, next) only when there is more than one page. The page is clamped when the list shrinks.
7. Footer: 36px high, 12px gap and 4px side padding inside the page gutter, right aligned: "Made with 💚 by ATP-MiNTo", a separator and an empty 28px slot. The GitHub button is a fixed 44px round link to `https://github.com/ATP-MiNTo/PRDashboards` (`target="_blank"`, `rel="noopener noreferrer"`, `aria-label="GitHub repository"`). A scroll, resize and size listener schedules one `requestAnimationFrame` per frame; `fabPlacement` returns either a spot 20px above the bottom edge in line with the slot's right edge (2px in) or, once the footer is fully visible, the slot. The result goes to the button as the custom properties `--fab-x`, `--fab-y` and `--fab-size` with `style.setProperty` in an effect, and the `snapped` class turns the circle and shadow off; the CSS transitions position and size. The button is always visible, also while scrolling and on narrow screens (on a screen of 390px it can cover a card's corner until the page scrolls; the design does the same). Its transitions (.45s on position and size) start after the first position settled, hover scales it to 1.06 while it floats, and it has the browser's own focus ring. Reduced motion turns the transitions and the scale off.

## Outside calls

None. Nothing here calls the server, GitHub or Jira, on mount or on a timer. The GitHub button is a plain link that the user follows.

## Rules and invariants

- No `style` attribute, no `<style>` block and no `style` prop in rendered markup (CSP `style-src 'self'`); dynamic state is a class, or a custom property set through the CSSOM after mount. A test checks the server-rendered footer for `style=`.
- Every control has an accessible name: "Sort", "Reverse sort order", "Hide #N", "Show, now X, N" and "Hidden PRs, now X, N" for the switches, a "Legend" list, "Pull requests per page", "Previous page", "Next page", "Page N" (`aria-current="page"` on the active one), "GitHub repository". Controls are at least 24px high (the sort, switch and page controls 32px); focus uses the global focus ring.
- Pure logic stays in `lib/sort.ts` and `lib/view.ts`: no network, no storage, no clock.
- Hiding is a view preference only; it never changes the snapshot, the stats strip or what the server does. A hidden PR still counts in "Open" and in the other stats.
- Animations stop under `prefers-reduced-motion: reduce`.

## Tests

- [web/src/lib/sort.test.ts](../../web/src/lib/sort.test.ts): each sort (including size and the fallback to the last commit time), reversal, ties, stacks kept together and ranked by best member, `sizeOf`.
- [web/src/lib/show.test.ts](../../web/src/lib/show.test.ts): the filtering table (each mode against each role set, reviewer included), `roleKind`, counts, `parseShowMode` (the legacy stored values), the missing-roles fallback, `groupsOf` with a mode and stacks.
- [web/src/lib/view.test.ts](../../web/src/lib/view.test.ts): paging (stacks not split, page size changes), ranges, page items, hidden modes, pruning, stored value checks, `listStats` (Blocked, Draft and Stale included, the three-day limit of Stale, and the tiles changing with the Show mode).
- [RollSwitch.test.tsx](../../web/src/components/Toolbar/RollSwitch.test.tsx): cycling, name and count, announcement, rolling label, the dot of a toned option, reduced motion, and the switch with a default (the turn without it, where a click from the default goes, right-click, Shift+click, the hint, a switch without one unchanged).
- [Hint.test.tsx](../../web/src/components/Toolbar/Hint.test.tsx): opens on focus and hover, closes on Escape, blur and leave, `aria-describedby`.
- [Toolbar.test.tsx](../../web/src/components/Toolbar/Toolbar.test.tsx): the sort options, the arrow, the switches with their counts, the dots of the Show modes, the hints and the legend with its three dot classes.
- [Pager.test.tsx](../../web/src/components/Toolbar/Pager.test.tsx): names, `aria-current`, single page, order of select and buttons.
- [Footer.test.tsx](../../web/src/components/Toolbar/Footer.test.tsx): link attributes, no style attribute in the markup, `fabPlacement`, snapping on scroll, always visible while scrolling.
- [ListControls.test.tsx](../../web/src/components/CommandTable/ListControls.test.tsx): the Show switch (order, counts, saved state and the legacy values, page reset, hint on focus, missing roles, the stat tiles following the mode), role classes and the legend, sort and reverse in the list, hide and unhide with persistence, pruning, saved mode and page size, fade-out, page reset, stacks across pages, the hint in place, the footer.
