import { describe, expect, it } from 'vitest';
import type { GithubToken, Snapshot } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import {
  kindOf,
  repoTokenTip,
  repoTokensOf,
  siteHost,
  sourceOf,
  tokenBadgeText,
  tokenNameProblem,
  tokenOfRepo,
} from './accounts';

const tok = (over: Partial<GithubToken> = {}): GithubToken => ({
  id: 'aaaaaaaa',
  name: 'Work',
  method: 'pat',
  fineGrained: true,
  login: 'minto',
  warning: null,
  needsReconnect: false,
  ...over,
});

describe('sourceOf', () => {
  it('is null while GitHub is not connected', () => {
    expect(sourceOf({ connected: false })).toBeNull();
  });

  it('tells the App, OAuth and the two kinds of token apart', () => {
    expect(sourceOf({ connected: true, method: 'app' })).toBe('app');
    expect(sourceOf({ connected: true })).toBe('app');
    expect(sourceOf({ connected: true, method: 'oauth' })).toBe('oauth');
    expect(sourceOf({ connected: true, method: 'pat' })).toBe('classic');
    expect(
      sourceOf({ connected: true, method: 'pat', warning: 'Fine-grained tokens only see...' }),
    ).toBe('fine-grained');
  });
});

describe('siteHost', () => {
  it('is the host of an https site', () => {
    expect(siteHost('https://acme.atlassian.net')).toBe('acme.atlassian.net');
  });

  it('is null for anything else', () => {
    expect(siteHost('http://acme.atlassian.net')).toBeNull();
    expect(siteHost('javascript:alert(1)')).toBeNull();
    expect(siteHost('not a url')).toBeNull();
  });
});

describe('kindOf', () => {
  it('tells the four kinds of token apart', () => {
    expect(kindOf(tok({ method: 'app', fineGrained: false }))).toBe('app');
    expect(kindOf(tok({ method: 'oauth', fineGrained: false }))).toBe('oauth');
    expect(kindOf(tok({ method: 'pat', fineGrained: false }))).toBe('classic');
    expect(kindOf(tok({ method: 'pat', fineGrained: true }))).toBe('fine-grained');
  });
});

describe('tokenNameProblem', () => {
  it('accepts 1 to 40 characters after trimming', () => {
    expect(tokenNameProblem('Work')).toBeNull();
    expect(tokenNameProblem('  Work  ')).toBeNull();
    expect(tokenNameProblem('x'.repeat(40))).toBeNull();
  });

  it('refuses an empty name, a long one and control characters', () => {
    expect(tokenNameProblem('')).toBe('Give the token a name.');
    expect(tokenNameProblem('   ')).toBe('Give the token a name.');
    expect(tokenNameProblem('x'.repeat(41))).toBe('Use at most 40 characters.');
    expect(tokenNameProblem('a\u0007b')).toBe('The name cannot have control characters.');
  });
});

describe('repoTokensOf', () => {
  it('keeps the token and role of each repository from the snapshot, once per repository', () => {
    const base = pr('a', 1).repo;
    const s: Snapshot = snapshot([
      pr('a', 1, {
        repo: { ...base, tokenId: 'aaaaaaaa', tokenName: 'Work', viewerPermission: 'WRITE' },
      }),
      pr('b', 2, { repo: { ...base, tokenName: 'Other' } }),
      pr('c', 3, { repo: { ...base, id: 'R2', nameWithOwner: 'acme/api' } }),
    ]);
    expect(repoTokensOf(s)).toEqual({
      R1: { tokenId: 'aaaaaaaa', tokenName: 'Work', viewerPermission: 'WRITE' },
      R2: {},
    });
  });
});

describe('tokenOfRepo', () => {
  const tokens = [tok(), tok({ id: 'bbbbbbbb', name: 'Home', method: 'app', fineGrained: false })];

  it('is null for a snapshot without tokens', () => {
    expect(tokenOfRepo(undefined, tokens)).toBeNull();
    expect(tokenOfRepo({}, tokens)).toBeNull();
  });

  it('finds the token by id and shows its current name (a rename shows at once)', () => {
    const renamed = [tok({ name: 'Client' })];
    expect(tokenOfRepo({ tokenId: 'aaaaaaaa', tokenName: 'Work' }, renamed)).toEqual({
      name: 'Client',
      kind: 'fine-grained',
      login: 'minto',
    });
  });

  it('finds it by name ignoring case when there is no id', () => {
    expect(tokenOfRepo({ tokenName: 'home' }, tokens)).toEqual({
      name: 'Home',
      kind: 'app',
      login: 'minto',
    });
  });

  it('keeps the name the snapshot gave for a token that is gone', () => {
    expect(tokenOfRepo({ tokenId: 'ffffffff', tokenName: 'Old' }, tokens)).toEqual({
      name: 'Old',
      kind: null,
      login: null,
    });
  });
});

describe('tokenBadgeText and repoTokenTip', () => {
  it('writes the name and the kind as text', () => {
    expect(tokenBadgeText({ name: 'Work', kind: 'fine-grained' })).toBe(
      'Work · Fine-grained token',
    );
    expect(tokenBadgeText({ name: 'Old', kind: null })).toBe('Old');
  });

  it('names the token, the account, the role and the one-owner limit of a fine-grained token', () => {
    const ref = { name: 'Work', kind: 'fine-grained' as const, login: 'minto' };
    expect(repoTokenTip(ref, 'WRITE')).toBe(
      'Fetched with Work (fine-grained token, minto). Your role here: Write. A fine-grained token works for one owner only.',
    );
  });

  it('adds the read-only note for READ and TRIAGE', () => {
    const ref = { name: 'Home', kind: 'app' as const, login: 'minto' };
    for (const role of ['READ', 'TRIAGE'] as const) {
      expect(repoTokenTip(ref, role)).toMatch(
        /^Fetched with Home \(GitHub App, minto\)\. Your role here: (Read|Triage)\. Read-only: Merge and Ready are not available here\. Add a token from this repository's owner in Accounts\.$/,
      );
    }
  });

  it('copes with no role and a token that is gone', () => {
    expect(repoTokenTip({ name: 'Old', kind: null, login: null }, undefined)).toBe(
      'Fetched with Old.',
    );
  });
});
