import type {
  MultiPrMode,
  RuleFilters,
  RuleMode,
  RuleTrigger,
  Snapshot,
  SyncRule,
  TicketWaiting,
} from '@/api/client';

export const MAX_RULES = 100;
export const MAX_HOURS = 720;

export const TRIGGERS: { value: RuleTrigger; label: string }[] = [
  { value: 'review_submitted', label: 'A review is submitted' },
  { value: 'review_approved', label: 'The pull request is approved' },
  { value: 'changes_requested', label: 'Changes are requested' },
  { value: 'review_requested', label: 'A review is requested' },
  { value: 'no_review_yet', label: 'No review yet' },
  { value: 'waiting_approvals', label: 'The pull request is waiting for more approvals' },
  { value: 'ready_for_review', label: 'The pull request is ready for review' },
  { value: 'commit_stale', label: 'The latest commit is older than N hours' },
  { value: 'ci_failing', label: 'CI is failing' },
  { value: 'ci_passing', label: 'CI is passing' },
  { value: 'merged', label: 'The pull request is merged' },
];

/** The hint of "Tickets shared by several PRs" (TECH_SPEC 7.5), with the third choice. */
export const MULTI_PR_HINT =
  'When a ticket is linked to several PRs. Default: follows "Multi-PR tickets move when" in the Repositories setting of the sidebar. All: the ticket moves only when every linked PR meets the rule. Any: the first PR that meets it moves the ticket.';

/** The two explicit modes as the "Tickets shared by several PRs" select words them. */
export const MODE_CHOICES: { value: Exclude<RuleMode, 'default'>; label: string }[] = [
  { value: 'all', label: 'When the last PR meets it' },
  { value: 'any', label: 'When the first PR meets it' },
];

/** The global "Multi-PR tickets move when" select of the sidebar. */
export const MULTI_PR_CHOICES: { value: MultiPrMode; label: string }[] = [
  { value: 'all', label: 'All PRs are merged' },
  { value: 'any', label: 'First PR is merged' },
];

/** The choices of the rule's select: first the one that follows the sidebar, showing what it is now. */
export function modeChoices(global: MultiPrMode): { value: RuleMode; label: string }[] {
  const now = MODE_CHOICES.find((m) => m.value === global) ?? MODE_CHOICES[0];
  return [
    {
      value: 'default',
      label: `Default (${now ? now.label.charAt(0).toLowerCase() + now.label.slice(1) : ''})`,
    },
    ...MODE_CHOICES,
  ];
}

const GLOB = /^[A-Za-z0-9._/*-]*$/;
const PROJECT = /^[A-Za-z0-9_]*$/;

export function triggerLabel(t: RuleTrigger): string {
  return TRIGGERS.find((x) => x.value === t)?.label ?? t;
}

export function emptyFilters(): RuleFilters {
  return { repo: '', branch: '', project: '', issueType: '', status: '' };
}

/** A new rule starts enabled and follows the default of the Repositories setting. */
export function newRule(): SyncRule {
  return {
    id: crypto.randomUUID(),
    name: '',
    enabled: true,
    trigger: 'merged',
    hours: 0,
    filters: emptyFilters(),
    target: '',
    mode: 'default',
  };
}

/** The name shown for a rule: its own, or a sentence built from the trigger and the target. */
export function ruleName(r: SyncRule): string {
  return r.name.trim() || `${triggerLabel(r.trigger)} to ${r.target || '...'}`;
}

export function modeLabel(m: RuleMode): string {
  return m === 'all' ? 'All' : m === 'any' ? 'Any' : 'Default';
}

export function describeFilters(f: RuleFilters): string[] {
  const out: string[] = [];
  if (f.repo) out.push(`repo ${f.repo}`);
  if (f.branch) out.push(`branch ${f.branch}`);
  if (f.project) out.push(`project ${f.project}`);
  if (f.issueType) out.push(`type ${f.issueType}`);
  if (f.status) out.push(`status ${f.status}`);
  return out;
}

/** The first thing wrong with a rule, or null. The server checks the same limits again. */
export function ruleProblem(r: SyncRule): string | null {
  if (r.target.trim() === '') return 'Choose the status to move tickets to.';
  if (r.target.length > 100 || r.name.length > 100)
    return 'Names and statuses are at most 100 characters.';
  if (
    r.trigger === 'commit_stale' &&
    !(Number.isInteger(r.hours) && r.hours >= 1 && r.hours <= MAX_HOURS)
  ) {
    return `Hours must be a whole number from 1 to ${MAX_HOURS}.`;
  }
  const { repo, branch, project, issueType, status } = r.filters;
  if (!GLOB.test(repo) || !GLOB.test(branch) || repo.length > 200 || branch.length > 200) {
    return 'Repo and branch use letters, digits and . _ / * - only.';
  }
  if (!PROJECT.test(project) || project.length > 50)
    return 'A Jira project key has letters, digits and _ only.';
  if (issueType.length > 100 || status.length > 100)
    return 'Issue types and statuses are at most 100 characters.';
  return null;
}

/**
 * The comparison key of a status name: lower case with everything but letters and digits dropped,
 * so "Re-open", "re open" and "REOPEN" match. A name with no letter or digit keeps its trimmed lower case form. The same rule as `rules.StatusKey` on the server.
 */
export const statusKey = (s: string): string => {
  const lower = s.trim().toLowerCase();
  return lower.replace(/[^\p{L}\p{N}]/gu, '') || lower;
};

/** The names without near-duplicates (by `statusKey`), the first spelling kept, sorted. */
function distinctStatuses(names: Iterable<string>): string[] {
  const byKey = new Map<string, string>();
  for (const n of names) {
    const key = statusKey(n);
    if (!byKey.has(key)) byKey.set(key, n);
  }
  return [...byKey.values()].sort((a, b) => a.localeCompare(b));
}

/** The Jira statuses seen on the tickets of the snapshot, as suggestions for a target status. */
export function statusesOf(s: Snapshot | undefined): string[] {
  const names: string[] = [];
  for (const pr of s?.prs ?? []) for (const t of pr.tickets) if (t.status) names.push(t.status);
  return distinctStatuses(names);
}

/** The Jira project keys seen on the tickets of the snapshot, sorted, as a fallback for the project filter. */
export function projectsOf(s: Snapshot | undefined): string[] {
  const keys = new Set<string>();
  for (const pr of s?.prs ?? []) for (const t of pr.tickets) if (t.project) keys.add(t.project);
  return [...keys].sort((a, b) => a.localeCompare(b));
}

/** Case-insensitive key of a Jira project key. */
export const projectKey = (s: string): string => s.toLowerCase();

/**
 * The names with the current value shown by its own spelling, so a dropdown can display it: an
 * option with the same `key` is replaced by `current`, and when there is none `current` is added.
 * Statuses compare by `statusKey` (the default), project keys by `projectKey`.
 */
export function withCurrent(
  names: readonly string[],
  current: string,
  key: (name: string) => string = statusKey,
): string[] {
  if (current === '') return [...names];
  const want = key(current);
  const at = names.findIndex((n) => key(n) === want);
  if (at < 0) return [...names, current];
  return names.map((n, i) => (i === at ? current : n));
}

/** The list to offer: the Jira list when it has names, else the names the snapshot shows. */
export function preferList(
  jira: readonly string[] | undefined,
  fallback: readonly string[],
): string[] {
  return jira && jira.length > 0 ? [...jira] : [...fallback];
}

const repoName = (nameWithOwner: string) => nameWithOwner.split('/').pop() ?? nameWithOwner;

/** "FE #34" for a pull request in a waiting hint. */
export function refText(r: { repo: string; number: number }): string {
  return `${repoName(r.repo)} #${r.number}`;
}

/** The hint under a ticket whose All rule waits on other pull requests. */
export function waitingText(w: Pick<TicketWaiting, 'total' | 'waitingOn'>): string {
  return `Waiting on ${w.waitingOn.length} of ${w.total} PRs: ${w.waitingOn.map(refText).join(', ')}`;
}
