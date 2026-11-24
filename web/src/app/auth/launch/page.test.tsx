import { render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import Launch from './page';

const createSession = vi.hoisted(() => vi.fn());

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  createSession,
}));

const replace = vi.fn();

beforeEach(() => {
  createSession.mockReset();
  replace.mockReset();
  const real = window.location;
  vi.stubGlobal('location', {
    get hash() {
      return real.hash;
    },
    get pathname() {
      return real.pathname;
    },
    get search() {
      return real.search;
    },
    replace,
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

describe('Launch page', () => {
  it('posts the token, wipes the fragment and goes home', async () => {
    window.history.replaceState(null, '', '/auth/launch/#t=tok_123');
    createSession.mockResolvedValue(undefined);
    render(<Launch />);
    await waitFor(() => expect(replace).toHaveBeenCalledWith('/'));
    expect(createSession).toHaveBeenCalledExactlyOnceWith('tok_123');
    expect(window.location.hash).toBe('');
    expect(window.location.pathname).toBe('/auth/launch/');
  });

  it('explains a missing token', async () => {
    window.history.replaceState(null, '', '/auth/launch/');
    render(<Launch />);
    expect(await screen.findByText(/no launch token/)).toBeInTheDocument();
    expect(createSession).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });

  it('explains a rejected token', async () => {
    window.history.replaceState(null, '', '/auth/launch/#t=old');
    createSession.mockRejectedValue(new ApiError(401, 'invalid_launch_token'));
    render(<Launch />);
    expect(await screen.findByText(/expired or was already used/)).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });

  it('explains a server failure', async () => {
    window.history.replaceState(null, '', '/auth/launch/#t=tok');
    createSession.mockRejectedValue(new TypeError('network'));
    render(<Launch />);
    expect(await screen.findByText(/Could not reach the server/)).toBeInTheDocument();
  });
});
