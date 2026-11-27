import { describe, expect, it } from 'vitest';
import { siteHost, sourceOf } from './accounts';

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
