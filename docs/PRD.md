# PR Dashboard: Product Requirements Document

Status: v1
Last updated: 2026-11-24
Technical design: [TECH_SPEC.md](TECH_SPEC.md)

## 1. Overview

A local web dashboard that lists every open pull request assigned to me, across all GitHub repositories, with review status, CI status and a merge button. Each PR is linked to its Jira ticket. Merging a PR also moves the ticket to a Jira status chosen by configurable rules, or picked by me when no rule matches.

## 2. Goals

- See all open PRs assigned to me in one place, regardless of repository or organization.
- Know at a glance whether a PR is ready to merge (review, CI, conflicts).
- Link each PR to the Jira ticket I am working on.
- Merge and update the ticket status in one action.

## 3. Non-Goals (v1)

- Creating PRs or editing tickets.
- Notifications or webhooks (a local app cannot receive webhooks without a tunnel).
- Multi-user support, accounts or sessions.
- Mobile or responsive layout.
- Hosted deployment.

## 4. User and Environment

- Single user (me).
- Runs locally on my machine. Not reachable from other devices.
- Tokens are stored locally (OS keychain, with an encrypted local file as fallback) and never leave the machine.

## 5. Functional Requirements

### 5.1 Authentication

- GitHub: OAuth App with `repo` and `read:org` scopes, with a personal access token as fallback when an org blocks OAuth apps. A GitHub App is not used because it only sees repos where it is installed, and the dashboard must cover all repos.
- Jira: Atlassian OAuth 2.0 (3LO) with `read:jira-work`, `write:jira-work` and `offline_access`.
- Both callbacks use `localhost` redirect URLs.
- Org-level restrictions (third-party app approval, SAML SSO authorization) are surfaced in the UI as "access blocked, authorize org" instead of failing silently.

### 5.2 PR List

Source: all open PRs where I am an assignee, across every repo I can access.

Each row shows:

- Repository and PR title (link to GitHub)
- Head branch and target branch
- Review status: approved, changes requested, pending
- CI status and its timestamp: passing, failing, running
- Latest commit time
- Draft and merge-conflict indicators
- Linked Jira tickets, one or more (key, title, current status, link)
- Matched rule per ticket (or "no rule matched")
- Merge method picker
- Merge action (see 5.4)

Columns, in order: Repo/PR, Ticket, Last commit, Review, CI, Method, Merge.

Data refresh: GitHub every minute and Jira every 5 minutes while the app runs (both configurable), plus a Sync button.

### 5.3 PR to Ticket Linking

Resolution order:

1. Parse the Jira key from the head branch name (for example `feat/INP-123-add-login-form`).
2. If none, parse the PR title.
3. If none, show a picker of Jira tickets assigned to me and let me choose manually.

Rules:

- All keys found are linked. A PR can have several tickets, and I can unlink any of them.
- A ticket can be linked to several PRs (for example BE, FE and proxy). See 5.9.
- A manual link is remembered for that PR.

### 5.4 Merge and Ticket Transition

Merge button behavior:

- Disabled when: draft, merge conflicts, required checks failing or pending, or required reviews missing. Branch protection is always respected and never bypassed.
- Merge opens a panel that lists every linked ticket, each with a transition preselected from the matching rule (or "Don't move"). I confirm each ticket, then merge.
- The button label says what will happen, for example "Merge & move to <status>" or "Merge & update 3 tickets".
- If the chosen transition requires extra fields (for example resolution), a small form is shown before merging.
- If the merge succeeds but the Jira transition fails, show an error with a retry action. The merge is not rolled back.
- If the PR has no linked ticket, I can merge without a transition after an explicit confirmation.

### 5.5 Status Sync Rules

Rules keep tickets in the right status during the whole PR lifecycle, not only at merge. A rule is "When <trigger> then move to <status>".

Triggers: review submitted, approved, changes requested, review requested, ready for review, latest commit older than N hours, CI failing or passing, merged.

Filters (all optional, combined with AND): repo, target branch (exact or pattern such as `release/*`), Jira project, issue type, current status.

A rule fires once when its trigger becomes true. It does not undo a manual move in Jira until something new happens. Events missed while the app was closed are caught up on the next start. Every move is logged.

Matching:

- Rules are ordered and the first match wins.
- If the data a rule depends on is unavailable (for example CI has not reported), that rule does not match.
- With no match, the ticket starts on "Don't move" in the merge panel and I pick a status there.
- Rules are global. A repo filter limits a rule to some repos.

### 5.6 Merge Method

- Each repo the dashboard has seen has a default merge method (squash, merge commit or rebase) and ordered branch rules (for example `main` and `production` to merge commit). The first matching rule wins.
- A newly seen repo gets the global default for new repos and opens its settings.
- Each PR row has a method picker to override the method for that merge.
- If a repo disallows the configured method, fall back to an allowed one and show a warning.
- Optional setting: delete the head branch after merge (off by default).

### 5.7 Settings

- Connected accounts (GitHub, Jira) and re-authorization
- Production branch names
- Default and production merge methods
- Repositories: default method and branch rules per repo, default for new repos
- Status sync rules (create, edit, reorder, enable, preview, delete)
- Multi-PR tickets: All or Any per rule (5.9)
- Polling intervals
- Delete branch after merge
- Theme: Light, Dark, Auto (follows time of day)

### 5.8 Stacked PRs

- A PR whose target branch is another open PR's head branch is shown grouped with it.
- It can only be merged after its base. A "merge whole stack" action is a later item.

### 5.9 Multi-PR Tickets

- When a ticket is linked to several PRs, each rule has a mode. All (default): the ticket moves only when every linked PR meets the rule. Any: the first PR that meets it moves the ticket.
- The Settings panel has a "Multi-PR tickets" section with a hint. Left-click a rule's chip for All, right-click for Any.
- A waiting ticket shows a hint such as "Waiting on 2 of 3 PRs: FE, proxy".

## 6. Non-Functional Requirements

### 6.1 Performance

- Fetch the full PR list with one paginated GitHub GraphQL search query (`is:pr is:open assignee:@me`). The query returns repo, branches, review decision, CI rollup, commit time, draft and mergeable state per PR. No per-PR follow-up calls.
- Resolve all Jira tickets with one batched JQL query. No per-PR Jira calls.
- Cache Jira transition lists per ticket and fetch them only when needed.
- Respect GitHub and Atlassian rate limits. Back off on 403 and 429 responses.
- Search results are capped at 1,000 items, which is acceptable for assigned PRs.

### 6.2 Security

- Tokens are stored in the OS keychain, or an encrypted file as fallback.
- Client secrets are kept in local configuration, never committed.
- The server binds to localhost only.
- OAuth state parameter is validated on callbacks.

### 6.3 Reliability

- Partial failures (one org blocked, Jira unreachable) do not break the whole list. Affected rows show a clear error state.
- Every write action (merge, transition) reports its result explicitly.

## 7. Risks and Open Items

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Orgs restrict third-party OAuth apps or require SAML SSO | Some PRs are not visible | Surface blocked orgs in the UI, document the approval steps |
| Atlassian OAuth requires a registered app and client secret | Setup effort | One-time setup, local configuration |
| Jira workflows differ per project and transitions may require fields | Transition failures | Fetch transitions per ticket, show required-field form, retry on failure |
| Repos disallow certain merge methods | Merge rejected | Check allowed methods, fall back with warning |
| Branch names without a ticket key | PR not linked | Title fallback, then manual picker |
| Search cap of 1,000 results | Truncated list | Not expected to be reached for assigned PRs, show a warning if hit |

## 8. Milestones (to be refined)

1. Plan the API call strategy and data model.
2. Choose the tech stack.
3. OAuth flows and token storage.
4. PR list with review and CI status.
5. Ticket linking.
6. Merge and transition flow with rules.
7. Settings panel.
8. Hardening: error states, rate limits, edge cases.

## 9. Success Criteria

- All open PRs assigned to me appear in one view, across all repos I can access.
- Review and CI status match what GitHub shows.
- A merge with a matching rule moves the ticket to the correct status in one click.
- No per-PR API calls are made when loading the list.
- Tokens never leave my machine.

## 10. Decisions Log

| Decision | Choice |
| --- | --- |
| App form | Local web dashboard |
| Users | Single user |
| Hosting | Local machine |
| Auth | OAuth for GitHub and Jira |
| PR to ticket link | Branch name, then PR title, then manual picker |
| Rule triggers | Review events, ready for review, commit age, CI, merged; filters on repo, branch, project, type, status |
| Status selection | Rule match, otherwise manual pick |
| Jira required fields | Show a small form |
| Refresh | Polling (GitHub 1 min, Jira 5 min) plus Sync |
| Merge method | Per repo default and branch rules, per-PR override |
| Repo scope | All repos with assigned PRs, no allowlist |
| Stack | Go server with embedded Next.js static export, SQLite, OS keychain |
| GitHub auth fallback | Personal access token |
| Rules | Status sync rules over the whole lifecycle, fire on change |
| Tickets per PR | Several, each confirmed in the merge panel |
| PRs per ticket | Several, All (default) or Any per rule |
| Stacked PRs | Grouped, child blocked until base merged |
| Security | Browser treated as a remote client: session, CSRF, Host and Origin checks |

## 11. Technical Design

Stack, data model, auth, storage and the API plan are in [TECH_SPEC.md](TECH_SPEC.md).
