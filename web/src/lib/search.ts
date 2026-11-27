import type { SnapshotPr } from '@/api/client';

/** The words of a search box: lower case, split on white space, a leading "#" of a PR number dropped. */
export function searchTerms(query: string): string[] {
  return query
    .toLowerCase()
    .split(/\s+/)
    .map((t) => (/^#\d/.test(t) ? t.slice(1) : t))
    .filter((t) => t !== '');
}

/** What a search looks at: the title, the repository, the PR number and the linked Jira keys. */
function haystack(pr: SnapshotPr): string {
  return [pr.title, pr.repo.nameWithOwner, String(pr.number), ...pr.tickets.map((t) => t.key)]
    .join('\n')
    .toLowerCase();
}

/**
 * Whether a pull request answers a search. Every word of the query must appear somewhere in the
 * title, repository, number or ticket keys, in any order. An empty query matches everything.
 */
export function matchesSearch(pr: SnapshotPr, terms: readonly string[]): boolean {
  if (terms.length === 0) return true;
  const text = haystack(pr);
  return terms.every((t) => text.includes(t));
}
