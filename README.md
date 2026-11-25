# PR Dashboards

A local dashboard that lists every open pull request assigned to you across GitHub, shows review
and CI status, links each pull request to its Jira tickets, and merges and moves those tickets in
one action. Status sync rules keep tickets in the right Jira status while their pull requests move
through review.

It runs on your own machine only. The app listens on `127.0.0.1`, and your GitHub and Jira
tokens stay in your OS keychain.

- Requirements: [docs/PRD.md](docs/PRD.md)
- Design: [docs/TECH_SPEC.md](docs/TECH_SPEC.md) and [docs/architecture.md](docs/architecture.md)
- One doc per feature: [docs/features/](docs/features/README.md)

## Contents

- [Requirements](#requirements)
- [Run the app](#run-the-app)
- [First-time setup](#first-time-setup)
- [User manual](#user-manual)
- [Troubleshooting](#troubleshooting)
- [Development](#development)

## Requirements

| Tool | Version | Notes |
| --- | --- | --- |
| Go | 1.26 or newer | Go downloads the 1.27.4 toolchain set in `server/go.mod` on its own |
| Node.js | 22 or newer | Only needed to build the UI, not to run it |
| pnpm | 10 | `corepack enable` installs it |
| Browser | A current Chrome, Edge, Firefox or Safari | |

The start scripts check these and stop with a clear message when one is missing.

## Run the app

Clone the repo, then run the start script for your system from anywhere:

| System | Command |
| --- | --- |
| macOS | Double-click `scripts/start.command` in Finder, or run `scripts/start.sh` in Terminal |
| Linux | `scripts/start.sh` |
| Windows | Double-click `scripts\start.cmd`, or run it from a terminal |

The script:

1. Checks Go, Node and pnpm.
2. Builds the UI and the `bin/prdash` binary, but only when the sources changed since the last
   build.
3. Starts the app and opens your browser on a one-time sign-in link.

The terminal window is the app. Keep it open while you use the dashboard, and press Ctrl+C to stop
it. Running the script again while the app is already running just opens a new browser tab on it.

The scripts never run `git pull` or switch branches. To update, pull the changes yourself and run
the script again.

### Options

Extra arguments are passed to the app:

```sh
scripts/start.sh --no-browser            # print the sign-in link instead of opening the browser
scripts/start.sh --data-dir ~/prdash-dev # keep data somewhere other than ~/.prdashboards
scripts/start.sh --port 4400             # another port (GitHub and Jira sign-in need 4317)
```

### Install it as an app

After the first start, use your browser's "Install app" (Chrome and Edge show it in the address
bar) to get a dock or taskbar icon. The installed app opens the dashboard in its own window. The
server still has to be running, so start it with the script first. If it is not running, the
window says so.

## First-time setup

You need two OAuth apps, one on GitHub and one on Atlassian. This is a one-time step.

### 1. Create the GitHub OAuth App

1. Go to GitHub, **Settings**, **Developer settings**, **OAuth Apps**, **New OAuth App**.
2. **Homepage URL**: `http://127.0.0.1:4317`
3. **Authorization callback URL**: `http://127.0.0.1:4317/oauth/github/callback`
4. Create it, then generate a client secret. Keep the client ID and secret for step 3.

### 2. Create the Atlassian OAuth 2.0 (3LO) app

1. Go to the [Atlassian developer console](https://developer.atlassian.com/console/myapps/) and
   create an **OAuth 2.0 integration**.
2. Under **Permissions**, add the **Jira API** with the scopes `read:jira-work`, `write:jira-work`
   and `read:jira-user`.
3. Under **Authorization**, set the callback URL to
   `http://127.0.0.1:4317/oauth/atlassian/callback`.
4. Under **Settings**, copy the client ID and secret.

### 3. Enter the credentials and sign in

1. Start the app. In the browser, open `http://127.0.0.1:4317/setup/`. The page also shows the
   exact callback URLs to register.
2. Paste the GitHub client ID and secret, save, then click **Connect GitHub** and approve.
3. Paste the Atlassian client ID and secret, save, then click **Connect Jira** and approve.
4. If your Atlassian account has several Jira sites, pick one in **Connected accounts**.

The dashboard fills in within a minute.

**No OAuth app for GitHub?** On the same page you can paste a personal access token instead. A
classic token needs the `repo` and `read:org` scopes. A fine-grained token works too, but it only
sees the repositories it was granted, and the app shows a warning for it.

**Organizations that block third-party apps.** If an organization restricts OAuth apps or needs
SAML SSO, its pull requests don't appear. Instead, a banner says "access blocked, authorize org",
with a link to authorize. The rest of the list still loads.

### Where your data lives

- Tokens and client secrets are in the OS keychain (macOS Keychain, Windows Credential Manager,
  or the Secret Service on Linux).
- With no keychain, the app keeps them in an encrypted file instead and asks for a passphrase in
  the terminal at each start.
- Settings, rules and the activity log are in `~/.prdashboards/data.db`. That folder is readable
  only by you, and the app refuses to start if other users can write to it.

## User manual

### The dashboard

Each row is one open pull request assigned to you. The columns, from left to right:

| Column | What it shows |
| --- | --- |
| Repo/PR | Repository, title (links to GitHub), head and target branch, draft and conflict flags |
| Ticket | Linked Jira tickets with key, status and title, and where each link came from |
| Last commit | Time of the latest commit. Hover for the exact time |
| Review | Approved, changes requested or review pending |
| CI | The overall CI state, then every check with its own status and a link to its run |
| Method | Merge method for this pull request: squash, merge commit or rebase |
| Merge | The Merge button, or the reason the pull request can't be merged yet |

Above the table:

- **Stat strip**: counts for open, ready, blocked, draft and stale pull requests.
- **Filter chips**: narrow the table to one of those groups.
- **Top bar**: when GitHub and Jira were last synced and when the next sync is due. **Sync**
  refreshes now.
- **Theme**: Light, Dark or Auto. Auto is light from 07:00 to 19:00 and dark otherwise.

The app checks GitHub every minute and Jira every 5 minutes while it runs. You can change both in
Settings.

### Stacked pull requests

A pull request whose target branch is another open pull request's branch is grouped under it with
a connector line. It can't be merged until its base is merged, and its Merge column says
"Merge after #N".

### Jira tickets

The app links a pull request to tickets in this order:

1. Keys in the branch name, for example `feat/INP-123-add-login-form`.
2. Keys in the pull request title.
3. Tickets you pick yourself.

In the Ticket cell:

- **×** unlinks a ticket from this pull request. The app remembers that.
- The picker lists your open Jira tickets. Use it to add a ticket, or to link one when the branch
  and title have none.
- A ticket can be linked to several pull requests, for example a backend, frontend and proxy
  change.

If Jira can't be reached, the cell says so and offers **Retry lookup**. The rest of the row still
works.

### Merging

Click **Merge** on a row to open the merge panel under it:

1. Every linked ticket is listed with the Jira status it will move to. A status sync rule picks
   it, otherwise it says "Don't move". Change any of them with the dropdown.
2. If a transition needs extra fields, such as a resolution, fill them in.
3. Tick each ticket to confirm it. Merge stays disabled until every ticket is confirmed.
4. Check the merge method and the delete-branch option, then click the button. Its label says
   what will happen, for example "Merge & move to Done" or "Merge & update 3 tickets".

The app checks everything again on the server just before it merges:

- The branch hasn't changed since the page loaded.
- GitHub reports the pull request as mergeable.
- The required reviews are there.
- A stacked pull request's base is merged.

If anything changed, the merge stops and the row refreshes. Branch protection is always respected.

If the merge works but a ticket move fails, the merge stays done. The failed ticket shows
**Retry**, and it also stays in **Settings**, **Activity** until you retry or dismiss it.

A pull request with no ticket can be merged after an extra confirmation.

### Merge method

The Method column shows the method for each pull request and where it came from:

- A branch rule for that repo, for example `main` uses merge commit. The first matching rule wins.
- Otherwise the repo default.
- For a repo the app has just seen, the global "default for new repos". The app also opens that
  repo's settings so you can review them.

If the repo doesn't allow that method, the app picks an allowed one and shows a warning. You can
pick a different method for one merge from the dropdown. Methods the repo doesn't allow are greyed
out.

### Status sync rules

Rules move tickets as their pull requests change, not only at merge. Set them in **Settings**,
**Status sync rules**. A rule reads "When *trigger* then move to *status*", with optional filters.

Triggers:

- A review is submitted, approved, or has changes requested
- A review is requested
- The pull request is marked ready for review
- The latest commit is older than N hours
- CI fails or passes
- The pull request is merged

Filters: repository, target branch (exact name or a pattern such as `release/*`), Jira project,
issue type and current status.

How rules behave:

- Rules are checked top to bottom, and the first one that matches wins. Drag a rule, or use its
  up and down buttons, to reorder.
- A rule fires once when its trigger becomes true. If you move a ticket back by hand in Jira, the
  app won't move it again until something new happens on the pull request.
- Things that happened while the app was closed are caught up at the next start.
- Before saving, **Preview** lists the tickets the rule would move now. Choose "Don't move existing
  tickets" to apply it only to new changes.
- If a transition needs extra fields, the app doesn't send it. It lists the ticket as "needs input"
  in **Activity** instead.

### Tickets with several pull requests

When one ticket is linked to several pull requests, each rule has a mode:

- **All** (default): the ticket moves only when every linked pull request meets the rule.
- **Any**: the first pull request that meets the rule moves the ticket.

Example: TIC-546 has BE #12, FE #34 and proxy #56, with the rule "when approved, move to Ready to
Merge".

- With All, the ticket moves only after the third approval. Until then its cell shows "Waiting on
  2 of 3 PRs: FE #34, proxy #56".
- With Any, the ticket moves as soon as BE #12 is approved.

To set the mode, open **Settings**, **Multi-PR tickets**:

- Left-click a rule's chip, or press Enter on it, for **All**.
- Right-click it, or press Shift+Enter, for **Any**.

Changing a mode never moves tickets by itself.

### Settings

The Settings button opens a panel on the right with these sections:

- **Connected accounts**: GitHub and Jira status, connect, reconnect, disconnect, and the Jira site
  to use.
- **Repositories**: every repo the dashboard has seen, each with its default method and branch
  rules. Also the default for new repos and "Delete branch after merge".
- **Status sync rules** and **Multi-PR tickets**: see above.
- **Polling**: how often GitHub and Jira are checked.
- **Theme**: Light, Dark or Auto.
- **Activity**: every merge and ticket move, failed moves to retry or dismiss, and "needs input"
  items.
- **Sessions**: **Sign out all browsers**.

## Troubleshooting

| What you see | What to do |
| --- | --- |
| "port 4317 is busy" | Another program uses the port. Stop it, or pass `--port` (sign-in needs 4317) |
| The browser shows "Open the app with scripts/start.sh" | Your session ended. Run the start script again; it opens a fresh sign-in link |
| "The launch link expired or was already used" | Launch links last 2 minutes and work once. Run the start script again |
| "access blocked, authorize org" banner | Click Authorize and approve the app for that organization |
| Jira shows "Reconnect" | The Jira sign-in expired. Click Reconnect |
| GitHub shows "rate limited" | The app waits for GitHub's limit to reset and slows its polling. The last data stays visible |
| "stdin is not a terminal" at start | There is no keychain, so the app needs a passphrase. Start it from a terminal |
| "data dir ... is writable by group or others" | Run `chmod 700 ~/.prdashboards` |

## Development

```sh
pnpm install                    # once, from the repo root
pnpm gen                        # regenerate Go and TS code from api/openapi.yaml
go test -race ./server/...      # server tests
pnpm -C web test                # UI tests
pnpm coverage-check             # 90% coverage gate for rules, linking, stacks and merge
pnpm docs-check                 # feature docs, links and index
pnpm e2e                        # Playwright against fake GitHub and Jira servers
```

[CLAUDE.md](CLAUDE.md) lists every command and the project rules. Each feature has its own doc in
[docs/features/](docs/features/README.md), and a change to a feature updates that doc in the same
pull request.

## License

[MIT](LICENSE)
