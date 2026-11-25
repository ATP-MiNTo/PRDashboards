import { describe, expect, it } from 'vitest';
import { appInstallUrl, isAppSlug } from './githubApp';

describe('isAppSlug', () => {
  it('accepts lowercase letters, digits and inner dashes', () => {
    for (const s of ['a', 'my-dash', 'app2', 'a'.repeat(100)]) expect(isAppSlug(s)).toBe(true);
  });

  it('rejects everything else', () => {
    for (const s of ['', 'Acme', '-a', 'a b', 'a/b', 'a_b', 'a'.repeat(101)])
      expect(isAppSlug(s)).toBe(false);
  });
});

describe('appInstallUrl', () => {
  it('rebuilds the link from the slug', () => {
    expect(appInstallUrl('https://github.com/apps/my-dash/installations/new')).toBe(
      'https://github.com/apps/my-dash/installations/new',
    );
  });

  it('rejects other hosts, schemes, paths and a missing value', () => {
    for (const u of [
      undefined,
      '',
      'http://github.com/apps/x/installations/new',
      'https://github.com.evil.example/apps/x/installations/new',
      'https://evil.example/apps/x/installations/new',
      'https://github.com/apps/X/installations/new',
      'https://github.com/apps/x/installations/new?x=1',
      'https://github.com/apps/x/installations/new#a',
      'https://user@github.com/apps/x/installations/new',
      'javascript:alert(1)',
    ])
      expect(appInstallUrl(u)).toBeNull();
  });
});
