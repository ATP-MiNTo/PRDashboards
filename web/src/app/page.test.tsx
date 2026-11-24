import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import Home from './page';

const getSession = vi.hoisted(() => vi.fn());

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  getSession,
}));

beforeEach(() => {
  getSession.mockReset();
});

describe('Home', () => {
  it('shows the signed-in state with the version', async () => {
    getSession.mockResolvedValue({
      csrfToken: 'c',
      expiresAt: '2026-12-24T00:00:00Z',
      version: '1.2.3',
    });
    render(<Home />);
    expect(await screen.findByText('Signed in')).toBeInTheDocument();
    expect(screen.getByText('Version 1.2.3')).toBeInTheDocument();
  });

  it('asks to open the app with the start script on 401', async () => {
    getSession.mockRejectedValue(new ApiError(401, 'unauthenticated'));
    render(<Home />);
    expect(await screen.findByText('Open the app with scripts/start.sh')).toBeInTheDocument();
  });

  it('shows an error when the server is unreachable', async () => {
    getSession.mockRejectedValue(new TypeError('network'));
    render(<Home />);
    expect(await screen.findByText(/Could not reach the server/)).toBeInTheDocument();
  });

  it('shows loading first', () => {
    getSession.mockReturnValue(new Promise(() => {}));
    render(<Home />);
    expect(screen.getByText('Loading')).toBeInTheDocument();
  });
});
