# Settings drawer

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 7.6 and 11. Related: [status-sync-rules.md](status-sync-rules.md), [installable-app.md](installable-app.md), [merge-method.md](merge-method.md), [merge-and-transition.md](merge-and-transition.md), [auth.md](auth.md), [theme.md](theme.md).

## Purpose

The drawer behind the Settings button: one place for the accounts, the merge method of each repo,
the defaults, the status sync rules, the Multi-PR modes, the sessions and the activity log.

## Data model

Reads and writes `Settings` (`deleteBranchAfterMerge`, `defaultMethodForNewRepos`, `polling`, `theme`), `RepoSettings` through `GET/PUT /api/repos/{id}`, reads and writes the status sync rules through `GET/PUT /api/rules` and `POST /api/rules/preview` ([status-sync-rules.md](status-sync-rules.md)), calls `POST /api/sessions/revoke-all`, and reads `GET /api/activity` and `GET /api/retries` ([merge-and-transition.md](merge-and-transition.md)). Drawer open state and the repo search text are component state.

## Code map

- [web/src/components/Settings/SettingsDrawer.tsx](../../web/src/components/Settings/SettingsDrawer.tsx): the dialog, focus trap, sections for delete branch, polling intervals and theme, and Sessions ("Sign out all browsers" ends every session including this one, then the drawer says how to sign in again).
- [RepositoriesSection.tsx](../../web/src/components/Settings/RepositoriesSection.tsx): search, default for new repos, one collapsible panel per repo with the ordered rule editor. [ActivitySection.tsx](../../web/src/components/Settings/ActivitySection.tsx): paginated activity (moves made by a rule, and "needs input" items) and the Jira moves waiting for a retry.
- [RulesSection.tsx](../../web/src/components/Settings/RulesSection.tsx), [RuleForm.tsx](../../web/src/components/Settings/RuleForm.tsx) and [MultiPrSection.tsx](../../web/src/components/Settings/MultiPrSection.tsx): the status sync rules and the Multi-PR tickets chips.
- [Settings.module.css](../../web/src/components/Settings/Settings.module.css). [web/src/components/Dropdown/Dropdown.tsx](../../web/src/components/Dropdown/Dropdown.tsx): the shared dropdown. [AccountsPanel.tsx](../../web/src/components/AccountsPanel.tsx): the Connected accounts section, reused as is.
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx) owns the open state, [TopBar.tsx](../../web/src/components/CommandTable/TopBar.tsx) has the button.

## Key flows

1. Settings opens a drawer from the right with a scrim. Focus moves into it and returns to the button on close. Tab wraps inside, Escape or a click on the scrim closes it. Slide and fade only run without `prefers-reduced-motion: reduce`.
2. Repositories: the search box filters by name. A repo that is new opens by itself with a "New" badge and a note. The panel has the default method and the ordered branch rules: a pattern input with the branches of that repo from the snapshot as suggestions, a method dropdown, up, down and remove, and Add rule (at most 50). Save sends the default and the whole list in one request with `clearNew: true`. A bad pattern disables Save.
3. The default for new repos, delete branch, polling intervals and theme save the whole `Settings` through `PUT /api/settings`.
4. Status sync rules: the ordered list has an enable checkbox, Edit, Delete, Up, Down and drag and drop reordering. Reorder, disable and delete save the whole list at once. Add and Edit open a form (name, trigger, hours for `commit_stale`, the five filters, a target status as free text with the statuses of the snapshot as suggestions, enabled). Save needs a preview of the rule as it stands, and when tickets would move now it asks "Move them" or "Don't move existing tickets". Enabling a disabled rule goes through the same form.
5. Multi-PR tickets: the title has an ⓘ hint (tooltip and accessible description) and one chip per rule. Left-click and Enter set All, right-click and Shift+Enter set Any. The chip has `aria-pressed` and a label with the mode, and saves with the baseline so nothing moves.
6. Activity loads 25 items at a time with "Load more". Jira moves that failed are listed above it with Retry and Dismiss. An `activity` event refreshes both.

## Outside calls

None to a provider. Saving a repo makes the server rebuild the snapshot from cached data.

## Rules and invariants

- A dropdown that is open closes on Escape without closing the drawer.
- No inline styles, no `dangerouslySetInnerHTML`.
- Rule patterns are checked in the browser and again by the server (schema pattern and length).

## Tests

- [SettingsDrawer.test.tsx](../../web/src/components/Settings/SettingsDrawer.test.tsx): sections, focus trap, Escape, scrim, preferences, activity paging, retries.
- [RulesSection.test.tsx](../../web/src/components/Settings/RulesSection.test.tsx) and [MultiPrSection.test.tsx](../../web/src/components/Settings/MultiPrSection.test.tsx): list, reorder by buttons and drag, preview and baseline choice, validation, chips and their keys, hint text.
- [RepositoriesSection.test.tsx](../../web/src/components/Settings/RepositoriesSection.test.tsx): list and search, new repo, rule editor, validation, save.
- [Dropdown.test.tsx](../../web/src/components/Dropdown/Dropdown.test.tsx): hover delay, click, keyboard, typeahead, disabled options, ARIA.
