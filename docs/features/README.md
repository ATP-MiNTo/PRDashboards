# Feature docs

One doc per feature. Together with [architecture.md](../architecture.md) they are the single source
of truth for how the app is built. They describe what the code does, not what the PRD wishes.
Where the two differ, the doc says so.

The index of all feature docs is the table in [architecture.md](../architecture.md).

## Workflow rules

- Before starting a task, read the feature docs it touches, and follow them.
- Any change that adds, changes or removes a feature's behaviour, data, flows, files or tests
  updates that feature's doc in the same PR.
- A new feature adds a new doc from the template below and updates its row in the
  `architecture.md` index.
- The PR's Change list links every feature doc it updated.
- When code and doc disagree, fix the one that is wrong in the same PR.

## Writing a doc

- Keep every heading from the template, in order. Write "None." under a heading with nothing to say.
- Link files with repo-relative links. Name functions and types. Do not use line numbers.
- Link to the PRD and TECH_SPEC instead of copying them.
- Status is one of `Built`, `Partial` or `Not built`.

## Template

```markdown
# <Feature name>

Status: Built | Partial | Not built. PRD: <link>. Spec: <link>. Related: <links>.

## Purpose

What it does for the user. Where the code differs from the PRD or spec, say so here.

## Data model

Tables, columns, API schemas and settings it owns or reads.

## Code map

Files grouped by area (server package, web), one line each on their role.

## Key flows

Numbered call paths from user action or refresh to result, naming the functions.

## Outside calls

Every GitHub or Jira call it makes, how often, and how it is batched.

## Rules and invariants

What the code enforces and what every change must keep true, including security checks.

## Tests

Unit, component and e2e files that cover it, linked.
```
