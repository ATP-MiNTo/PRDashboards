import { describe, expect, it, vi } from 'vitest';
import { registerServiceWorker } from './pwa';

describe('registerServiceWorker', () => {
  it('registers /sw.js with the root scope', async () => {
    const register = vi.fn().mockResolvedValue({ scope: '/' });
    const reg = await registerServiceWorker({ serviceWorker: { register } } as never);
    expect(register).toHaveBeenCalledWith('/sw.js', { scope: '/' });
    expect(reg).toEqual({ scope: '/' });
  });

  it('does nothing without service worker support', async () => {
    expect(await registerServiceWorker({} as never)).toBeNull();
    expect(await registerServiceWorker(undefined)).toBeNull();
  });

  it('survives a failed registration', async () => {
    const register = vi.fn().mockRejectedValue(new Error('blocked'));
    expect(await registerServiceWorker({ serviceWorker: { register } } as never)).toBeNull();
  });
});
