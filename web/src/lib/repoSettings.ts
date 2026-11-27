import type {
  BranchRule,
  DeleteBranchChoice,
  MergeMethodName,
  RepoSettings,
  RepoUpdate,
  Snapshot,
} from '@/api/client';
import { METHOD_NAMES, METHOD_ORDER } from './merge';

/** The branch rules a repository gets when it is first seen (`store.SeedRules`). They are not a customization. */
export const SEED_RULES: readonly BranchRule[] = [
  { pattern: 'main', method: 'merge' },
  { pattern: 'production', method: 'merge' },
];

export const MAX_RULES = 50;
export const MIN_APPROVALS = 1;
export const MAX_APPROVALS = 10;

const PATTERN = /^[A-Za-z0-9._/*-]{1,200}$/;

/** Letters, digits and . _ / * - only, 1 to 200 characters: what the server accepts. */
export const validPattern = (p: string): boolean => PATTERN.test(p);

/** The unsaved choices of the repository dialog. */
export interface RepoDraft {
  /** "default" follows the default method of Settings. */
  method: MethodChoice;
  /** The method kept while the repository follows the default, so "Use default" and back loses nothing. */
  ownMethod: MergeMethodName;
  deleteBranch: DeleteBranchChoice;
  approvals: number;
  rules: BranchRule[];
}

export type MethodChoice = 'default' | MergeMethodName;

export const DELETE_CHOICES: readonly DeleteBranchChoice[] = ['default', 'yes', 'no'];

export const draftOf = (r: RepoSettings): RepoDraft => ({
  method: r.inheritMethod ? 'default' : r.defaultMethod,
  ownMethod: r.defaultMethod,
  deleteBranch: r.deleteBranch,
  approvals: r.approvalsRequired,
  rules: r.branchRules,
});

/** The update that saves a draft. The repository is marked as seen. */
export const updateOf = (d: RepoDraft): RepoUpdate => ({
  defaultMethod: d.method === 'default' ? d.ownMethod : d.method,
  inheritMethod: d.method === 'default',
  deleteBranch: d.deleteBranch,
  approvalsRequired: d.approvals,
  branchRules: d.rules,
  clearNew: true,
});

/** Whether the draft can be saved: every pattern is valid and there are not too many rules. */
export const draftValid = (d: RepoDraft): boolean =>
  d.rules.length <= MAX_RULES && d.rules.every((r) => validPattern(r.pattern));

/** Whether the draft differs from what is stored. */
export function draftChanged(d: RepoDraft, r: RepoSettings): boolean {
  const s = draftOf(r);
  return (
    d.method !== s.method ||
    (d.method !== 'default' && d.method !== s.ownMethod) ||
    d.deleteBranch !== s.deleteBranch ||
    d.approvals !== s.approvals ||
    JSON.stringify(d.rules) !== JSON.stringify(s.rules)
  );
}

/** The text of the delete-branch roll: the choice, or the default and what it is now. */
export function deleteLabel(choice: DeleteBranchChoice, globalDelete: boolean | undefined): string {
  if (choice === 'yes') return 'Yes';
  if (choice === 'no') return 'No';
  return globalDelete === undefined
    ? 'Use default'
    : `Use default (${globalDelete ? 'Yes' : 'No'})`;
}

/** The text of the method roll: the method, or the default and what it is now. */
export function methodLabel(
  choice: MethodChoice,
  globalMethod: MergeMethodName | undefined,
): string {
  if (choice !== 'default') return METHOD_NAMES[choice];
  return globalMethod === undefined ? 'Use default' : `Use default (${METHOD_NAMES[globalMethod]})`;
}

/** What the repository allows, as the snapshot says. A repository with no pull request there allows all. */
export type AllowedMethods = Readonly<Record<MergeMethodName, boolean>>;

const ALL_ALLOWED: AllowedMethods = { squash: true, merge: true, rebase: true };

export function allowedOf(s: Snapshot, repoId: string): AllowedMethods {
  const repo = s.prs.find((p) => p.repo.id === repoId)?.repo;
  return repo
    ? { squash: repo.squashAllowed, merge: repo.mergeAllowed, rebase: repo.rebaseAllowed }
    : ALL_ALLOWED;
}

/**
 * The choices the method roll turns through: the default, then the methods the repository allows.
 * A method the repository no longer allows stays in the list while it is the current choice, so it
 * can be read, and leaves it once it is changed.
 */
export function methodChoices(allowed: AllowedMethods, current: MethodChoice): MethodChoice[] {
  return ['default', ...METHOD_ORDER.filter((m) => allowed[m] || m === current)];
}

/** One step of the approvals field, held between 1 and 10. */
export function stepApprovals(n: number, dir: 1 | -1): number {
  return Math.min(MAX_APPROVALS, Math.max(MIN_APPROVALS, n + dir));
}

/**
 * Whether a repository has settings of its own: a delete-branch choice, a method of its own, more
 * than one approval, or branch rules other than the ones every repository starts with. It is the
 * blue gear in the sidebar.
 */
export function isCustom(r: RepoSettings): boolean {
  return (
    r.deleteBranch !== 'default' ||
    !r.inheritMethod ||
    r.approvalsRequired !== MIN_APPROVALS ||
    JSON.stringify(r.branchRules) !== JSON.stringify(SEED_RULES)
  );
}

/** The branches the snapshot knows for a repository, as suggestions for a rule pattern. */
export function branchesOf(s: Snapshot, repoId: string): string[] {
  const names = new Set<string>();
  for (const pr of s.prs) {
    if (pr.repo.id !== repoId) continue;
    names.add(pr.baseRef);
    names.add(pr.headRef);
  }
  return [...names].sort();
}
