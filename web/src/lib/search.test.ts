import { describe, expect, it } from 'vitest';
import { pr, snapshot, ticket } from '@/test-fixtures';
import { groupsOf } from './snapshot';
import { matchesSearch, searchTerms } from './search';

const row = pr('a', 490, {
  title: 'Add CSV import preview step',
  repo: {
    id: 'R9',
    nameWithOwner: 'acme/web-console',
    isNew: false,
    squashAllowed: true,
    mergeAllowed: true,
    rebaseAllowed: true,
  },
  tickets: [ticket('PAY-420'), ticket('WEB-7')],
});

describe('searchTerms', () => {
  it.each([
    ['', []],
    ['   ', []],
    ['CSV', ['csv']],
    ['  csv   import ', ['csv', 'import']],
    ['#490', ['490']],
    ['#hash', ['#hash']],
  ])('%j gives %j', (query, terms) => {
    expect(searchTerms(query)).toEqual(terms);
  });
});

describe('matchesSearch', () => {
  it.each([
    ['an empty search', '', true],
    ['the title, in any case', 'csv IMPORT', true],
    ['a part of the title', 'prev', true],
    ['the repository', 'web-console', true],
    ['the owner', 'acme/', true],
    ['the number', '490', true],
    ['the number with a hash', '#490', true],
    ['a Jira key', 'pay-420', true],
    ['the second Jira key', 'WEB-7', true],
    ['words from different fields', 'csv web-console pay-420', true],
    ['words in another order', 'pay-420 csv', true],
    ['a word that is nowhere', 'csv nothing', false],
    ['another number', '491', false],
    ['an unknown key', 'PAY-421', false],
  ])('%s', (_name, query, expected) => {
    expect(matchesSearch(row, searchTerms(query))).toBe(expected);
  });

  it('does not look at the branch names or the review state', () => {
    expect(matchesSearch(row, searchTerms('feat/490'))).toBe(false);
    expect(matchesSearch(row, searchTerms('approved'))).toBe(false);
  });
});

describe('groupsOf with a search', () => {
  const s = snapshot([
    pr('a', 1, { title: 'Login form' }),
    pr('b', 2, { title: 'Billing page', tickets: [ticket('PAY-9')] }),
    pr('c', 3, { title: 'Billing export' }),
  ]);

  it('keeps the matching pull requests in their order', () => {
    const groups = groupsOf(s, 'all', 'all', searchTerms('billing'));
    expect(groups.map((g) => g.ids)).toEqual([['b'], ['c']]);
  });

  it('combines with the filter and finds by ticket key', () => {
    expect(groupsOf(s, 'all', 'all', searchTerms('pay-9')).map((g) => g.ids)).toEqual([['b']]);
    expect(groupsOf(s, 'ready', 'all', searchTerms('pay-9')).map((g) => g.ids)).toEqual([['b']]);
    expect(groupsOf(s, 'running', 'all', searchTerms('pay-9'))).toEqual([]);
  });

  it('lists everything for an empty search', () => {
    expect(groupsOf(s, 'all', 'all', []).length).toBe(3);
  });
});
