import { screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { renderWithClient } from '@/test-utils';
import Accounts from './page';

const getSession = vi.hoisted(() => vi.fn());

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  getSession,
}));

vi.mock('@/components/AccountsPanel', () => ({ AccountsPanel: () => <p>accounts panel</p> }));

beforeEach(() => {
  getSession.mockReset();
});

describe('Accounts page', () => {
  it('shows the signed-in state with the version', async () => {
    getSession.mockResolvedValue({
      csrfToken: 'c',
      expiresAt: '2026-12-24T00:00:00Z',
      version: '1.2.3',
    });
    renderWithClient(<Accounts />);
    expect(await screen.findByText('Signed in')).toBeInTheDocument();
    expect(screen.getByText('Version 1.2.3')).toBeInTheDocument();
    expect(screen.getByText('accounts panel')).toBeInTheDocument();
  });

  it('asks to open the app with the start script on 401', async () => {
    getSession.mockRejectedValue(new ApiError(401, 'unauthenticated'));
    renderWithClient(<Accounts />);
    expect(await screen.findByText('Open the app with scripts/start.sh')).toBeInTheDocument();
  });

  it('shows an error when the server is unreachable', async () => {
    getSession.mockRejectedValue(new TypeError('network'));
    renderWithClient(<Accounts />);
    expect(await screen.findByText(/Could not reach the server/)).toBeInTheDocument();
  });

  it('shows loading first', () => {
    getSession.mockReturnValue(new Promise(() => {}));
    renderWithClient(<Accounts />);
    expect(screen.getByText('Loading')).toBeInTheDocument();
  });
});
