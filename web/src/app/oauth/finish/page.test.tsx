import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Finish from './page';

const completeOAuth = vi.hoisted(() => vi.fn());

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  completeOAuth,
}));

const replace = vi.fn();

beforeEach(() => {
  completeOAuth.mockReset().mockResolvedValue(undefined);
  replace.mockReset();
  vi.spyOn(window, 'location', 'get').mockReturnValue({
    ...window.location,
    search: '?provider=github&code=abc&state=xyz',
    pathname: '/oauth/finish/',
    replace,
  } as unknown as Location);
});

afterEach(() => window.history.replaceState(null, '', '/'));

describe('OAuth finish page', () => {
  it('posts the code once, clears the query and moves on', async () => {
    const replaceState = vi.spyOn(window.history, 'replaceState');
    render(<Finish />);
    expect(screen.getByText('Finishing sign-in')).toBeInTheDocument();
    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith('/auth/done/?result=ok&provider=github'),
    );
    expect(replaceState).toHaveBeenCalledWith(null, '', '/oauth/finish/');
    expect(completeOAuth).toHaveBeenCalledTimes(1);
    expect(completeOAuth).toHaveBeenCalledWith({ provider: 'github', code: 'abc', state: 'xyz' });
  });
});
