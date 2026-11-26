# Settings

Status: Built. PRD: [PRD.md](../PRD.md). Spec: [TECH_SPEC.md](../TECH_SPEC.md) sections 7.6 and 11. Related: [status-sync-rules.md](status-sync-rules.md), [installable-app.md](installable-app.md), [merge-method.md](merge-method.md), [merge-and-transition.md](merge-and-transition.md), [auth.md](auth.md), [theme.md](theme.md).

## Purpose

The Settings content, shown in the left sidebar ([sidebar-shell.md](sidebar-shell.md)) and toggled by the burger:
one place for the merge method of each repo, the defaults, the status sync rules, the Multi-PR modes,
the sessions, the theme and the activity log. The accounts have their own panel on the right, behind
the Account button ([auth.md](auth.md)), built on the shared `SidePanel`.

## Data model

Reads and writes `Settings` (`deleteBranchAfterMerge`, `defaultMethodForNewRepos`, `polling`, `theme`, `excludedStatuses`), `RepoSettings` through `GET/PUT /api/repos/{id}` and `PUT /api/repos` (several repos in one request), reads `GET /api/jira/options` (statuses and project keys for the rule form), reads and writes the status sync rules through `GET/PUT /api/rules` and `POST /api/rules/preview` ([status-sync-rules.md](status-sync-rules.md)), calls `POST /api/sessions/revoke-all`, and reads `GET /api/activity` and `GET /api/retries` ([merge-and-transition.md](merge-and-transition.md)). Sidebar open state ([sidebar-shell.md](sidebar-shell.md)), the fold of the Repositories section, the repo search text and the unsaved repo drafts are component state.

## Code map

- [web/src/components/SidePanel/SidePanel.tsx](../../web/src/components/SidePanel/SidePanel.tsx): the shared shell of the Account panel, with scrim, `role=dialog`, header with the title and Close button, focus trap, Escape and focus restore. Props `open`, `onClose`, `title`, `closeLabel`, `children`.
- [web/src/components/Settings/SettingsContent.tsx](../../web/src/components/Settings/SettingsContent.tsx): `SettingsContent`, rendered in the sidebar body (the sidebar supplies the "Settings" title and the collapse button), with, in order, Repositories, Preferences (delete branch, polling intervals), Status sync rules, Multi-PR tickets, Sessions ("Sign out all browsers" ends every session including this one, then the sidebar says how to sign in again), Theme and Activity.
- [RepositoriesSection.tsx](../../web/src/components/Settings/RepositoriesSection.tsx): a foldable section with search, default for new repos, one collapsible panel per repo with the ordered rule editor, and Save all. The drafts of every repo live in the section. [ActivitySection.tsx](../../web/src/components/Settings/ActivitySection.tsx): paginated activity (moves made by a rule, and "needs input" items) and the Jira moves waiting for a retry.
- [RulesSection.tsx](../../web/src/components/Settings/RulesSection.tsx), [RuleForm.tsx](../../web/src/components/Settings/RuleForm.tsx) and [MultiPrSection.tsx](../../web/src/components/Settings/MultiPrSection.tsx): the status sync rules and the Multi-PR tickets chips.
- [Settings.module.css](../../web/src/components/Settings/Settings.module.css). [web/src/components/Dropdown/Dropdown.tsx](../../web/src/components/Dropdown/Dropdown.tsx): the shared dropdown (scrolls past 280px, and lists of more than 12 options get a search box). [MultiSelect.tsx](../../web/src/components/MultiSelect/MultiSelect.tsx): the searchable multi-select, with `usePopover.ts` in the Dropdown folder for placing the popover. [AccountsPanel.tsx](../../web/src/components/AccountsPanel.tsx): the Connected accounts content, shown in the Account panel and on `/accounts/`.
- [CommandTable.tsx](../../web/src/components/CommandTable/CommandTable.tsx) owns the sidebar and the Account panel, [TopBar.tsx](../../web/src/components/CommandTable/TopBar.tsx) has the Account button, and the burger ([Burger.tsx](../../web/src/components/Shell/Burger.tsx)) toggles the sidebar.
- Server: [handlers_repos.go](../../server/internal/api/handlers_repos.go) (`PutRepos`), [store/repos.go](../../server/internal/store/repos.go) (`PutRepos`), [handlers_snapshot.go](../../server/internal/api/handlers_snapshot.go) (`GetJiraOptions`).

## Key flows

1. The burger opens the sidebar on the left, a column on wide screens and a modal overlay on narrow ones (focus, Tab, Escape, scrim and animation rules are in [sidebar-shell.md](sidebar-shell.md)). The Account panel is independent and opens from the right.
2. Repositories: the title is a disclosure button (`aria-expanded`, collapsed by default) that hides the default for new repos, the search, the list and Save all. The search box filters by name. A repo that is new opens by itself with a "New" badge and a note. The panel has the default method and the ordered branch rules: a pattern input with the branches of that repo from the snapshot as suggestions, a method dropdown, up, down and remove. The buttons "Add rule" (at most 50) and "Save" sit in a group named after the repo. Save sends the default and the whole list in one request (`PUT /api/repos/{id}`) with `clearNew: true`. A bad pattern disables Save. "Save all" sends every repo with a changed draft, and every new repo, in one request (`PUT /api/repos`). It is disabled when there is nothing to save, when a changed draft is invalid or while a save is pending. Drafts are kept across refetches. A saved draft is dropped once its request succeeds, and drafts of other repos stay.
3. The default for new repos, delete branch, polling intervals, theme and the excluded statuses save the whole `Settings` through `PUT /api/settings`. The two polling selects (labels "GitHub" and "Jira") share one line with a hidden "|" between them. Theme is its own section after Sessions, with a sun/moon swap and an Auto checkbox ([theme.md](theme.md)).
4. Status sync rules: the ordered list has an enable checkbox, Edit, Delete, Up, Down and drag and drop reordering. Reorder, disable and delete save the whole list at once. Add and Edit open a form (name, trigger, hours for `commit_stale`, the five filters, a target status, enabled). The target, the current-status filter and the Jira project filter are dropdowns fed by `GET /api/jira/options` ([status-sync-rules.md](status-sync-rules.md)). Under the list, "Exclude statuses" is a searchable multi-select of the statuses whose tickets are never moved (near-duplicate statuses count as one, see [status-sync-rules.md](status-sync-rules.md)); the picks show as removable chips. Save needs a preview of the rule as it stands, and when tickets would move now it asks "Move them" or "Don't move existing tickets". Enabling a disabled rule goes through the same form.
5. Multi-PR tickets: the title has an ⓘ hint (tooltip and accessible description) and one chip per rule. Left-click and Enter set All, right-click and Shift+Enter set Any. The chip has `aria-pressed` and a label with the mode, and saves with the baseline so nothing moves.
6. Activity loads 25 items at a time with "Load more". Jira moves that failed are listed above it with Retry and Dismiss. An `activity` event refreshes both.

## Outside calls

The Settings content mounts when the sidebar first opens, which on a wide screen is page load, so only local calls may run then. On mount it reads `GET /api/settings`, `GET /api/repos`, `GET /api/rules`, `GET /api/activity`, `GET /api/retries` and the snapshot from the query cache. These handlers read the database and make no GitHub or Jira call. `GET /api/jira/options` is the only Settings request that reaches Jira (`GET /rest/api/3/status`, cached 10 minutes on the server). `RulesSection` enables it only when the rule form opens (Add rule, Edit, or enabling a rule) or the Exclude statuses list is first opened (`MultiSelect` `onOpen`), never on mount and never on a timer. The content stays mounted after a collapse, so unsaved drafts survive. Saving a repo, or several, makes the server rebuild the snapshot from cached data, with one Jira lookup for tickets it has not seen.

## Rules and invariants

- A dropdown or multi-select that is open closes on Escape without closing the sidebar.
- No inline styles, no `dangerouslySetInnerHTML`.
- Rule patterns are checked in the browser and again by the server (schema pattern and length).

## Tests

- [SidePanel.test.tsx](../../web/src/components/SidePanel/SidePanel.test.tsx): dialog, close label, Escape, scrim, focus restore and trap (the Account panel).
- [SettingsContent.test.tsx](../../web/src/components/Settings/SettingsContent.test.tsx): sections and their order, preferences and the polling line, theme, activity paging, retries.
- [RulesSection.test.tsx](../../web/src/components/Settings/RulesSection.test.tsx) and [MultiPrSection.test.tsx](../../web/src/components/Settings/MultiPrSection.test.tsx): list, reorder by buttons and drag, preview and baseline choice, validation, the Jira dropdowns and their fallback, Exclude statuses (open, search, toggle, remove a chip, fallback, save failure), chips and their keys, hint text.
- [RepositoriesSection.test.tsx](../../web/src/components/Settings/RepositoriesSection.test.tsx): fold, list and search, new repo, rule editor, validation, save, Save all (one request, disabled states, drafts kept).
- [Dropdown.test.tsx](../../web/src/components/Dropdown/Dropdown.test.tsx): hover delay, click, keyboard, typeahead, disabled options, ARIA, the search box of long lists, scrolling the active option into view, Escape.
- [MultiSelect.test.tsx](../../web/src/components/MultiSelect/MultiSelect.test.tsx): summary, click to open, search, toggle order, `max`, keys (arrows, Enter, Space, Escape, Tab), outside click, chips, `onOpen`.
