import { describe, expect, it, vi } from 'vitest';
import { parseLaunchToken, runLaunch, type LaunchDeps } from './launch';

describe('parseLaunchToken', () => {
  it('reads t from the fragment', () => {
    expect(parseLaunchToken('#t=abc_DEF-123')).toBe('abc_DEF-123');
    expect(parseLaunchToken('t=abc')).toBe('abc');
    expect(parseLaunchToken('#x=1&t=abc')).toBe('abc');
  });

  it.each([
    ['empty', ''],
    ['hash only', '#'],
    ['no t', '#x=1'],
    ['empty t', '#t='],
    ['bad characters', '#t=a b'],
    ['markup', '#t=<script>'],
    ['padding', '#t=abc=='],
    ['too long', `#t=${'a'.repeat(129)}`],
  ])('rejects %s', (_name, hash) => {
    expect(parseLaunchToken(hash)).toBeNull();
  });

  it('accepts the longest allowed token', () => {
    expect(parseLaunchToken(`#t=${'a'.repeat(128)}`)).toBe('a'.repeat(128));
  });
});

function deps(overrides: Partial<LaunchDeps> = {}) {
  const calls: string[] = [];
  const d: LaunchDeps = {
    hash: '#t=tok',
    clearHash: vi.fn(() => void calls.push('clear')),
    createSession: vi.fn(async () => void calls.push('create')),
    isRejection: (e) => e instanceof Error && e.message === 'rejected',
    goHome: vi.fn(() => void calls.push('home')),
    ...overrides,
  };
  return { d, calls };
}

describe('runLaunch', () => {
  it('clears the fragment before the request, then goes home', async () => {
    const { d, calls } = deps();
    await expect(runLaunch(d)).resolves.toBe('signed-in');
    expect(calls).toEqual(['clear', 'create', 'home']);
    expect(d.createSession).toHaveBeenCalledWith('tok');
  });

  it('clears the fragment and stays put without a token', async () => {
    const { d } = deps({ hash: '#nope' });
    await expect(runLaunch(d)).resolves.toBe('missing-token');
    expect(d.clearHash).toHaveBeenCalled();
    expect(d.createSession).not.toHaveBeenCalled();
    expect(d.goHome).not.toHaveBeenCalled();
  });

  it('reports a rejected token', async () => {
    const { d } = deps({ createSession: vi.fn().mockRejectedValue(new Error('rejected')) });
    await expect(runLaunch(d)).resolves.toBe('rejected');
    expect(d.goHome).not.toHaveBeenCalled();
  });

  it('reports other failures', async () => {
    const { d } = deps({ createSession: vi.fn().mockRejectedValue(new Error('network')) });
    await expect(runLaunch(d)).resolves.toBe('failed');
    expect(d.goHome).not.toHaveBeenCalled();
  });
});
