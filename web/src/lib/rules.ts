import type {
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
  { value: 'ready_for_review', label: 'The pull request is ready for review' },
  { value: 'commit_stale', label: 'The latest commit is older than N hours' },
  { value: 'ci_failing', label: 'CI is failing' },
  { value: 'ci_passing', label: 'CI is passing' },
  { value: 'merged', label: 'The pull request is merged' },
];

/** The hint of the Multi-PR tickets section (TECH_SPEC 7.5), word for word. */
export const MULTI_PR_HINT =
  'When a ticket is linked to several PRs. All: the ticket moves only when every linked PR meets the rule. Any: the first PR that meets it moves the ticket. Left-click for All, right-click for Any.';

const GLOB = /^[A-Za-z0-9._/*-]*$/;
const PROJECT = /^[A-Za-z0-9_]*$/;

export function triggerLabel(t: RuleTrigger): string {
  return TRIGGERS.find((x) => x.value === t)?.label ?? t;
}

export function emptyFilters(): RuleFilters {
  return { repo: '', branch: '', project: '', issueType: '', status: '' };
}

/** A new rule starts enabled and in mode All. */
export function newRule(): SyncRule {
  return {
    id: crypto.randomUUID(),
    name: '',
    enabled: true,
    trigger: 'merged',
    hours: 0,
    filters: emptyFilters(),
    target: '',
    mode: 'all',
  };
}

/** The name shown for a rule: its own, or a sentence built from the trigger and the target. */
export function ruleName(r: SyncRule): string {
  return r.name.trim() || `${triggerLabel(r.trigger)} to ${r.target || '...'}`;
}

export function modeLabel(m: RuleMode): string {
  return m === 'all' ? 'All' : 'Any';
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

/** The Jira statuses seen on the tickets of the snapshot, as suggestions for a target status. */
export function statusesOf(s: Snapshot | undefined): string[] {
  const names = new Set<string>();
  for (const pr of s?.prs ?? []) for (const t of pr.tickets) if (t.status) names.add(t.status);
  return [...names].sort((a, b) => a.localeCompare(b));
}

/** The Jira project keys seen on the tickets of the snapshot, sorted, as a fallback for the project filter. */
export function projectsOf(s: Snapshot | undefined): string[] {
  const keys = new Set<string>();
  for (const pr of s?.prs ?? []) for (const t of pr.tickets) if (t.project) keys.add(t.project);
  return [...keys].sort((a, b) => a.localeCompare(b));
}

/** The names with the current value added when it is missing, so a dropdown can still show it. */
export function withCurrent(names: readonly string[], current: string): string[] {
  return current !== '' && !names.includes(current) ? [...names, current] : [...names];
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
