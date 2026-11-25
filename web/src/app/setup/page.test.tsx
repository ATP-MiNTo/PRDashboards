import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/api/client';
import { renderWithClient } from '@/test-utils';
import Setup from './page';

const api = vi.hoisted(() => ({
  getSetup: vi.fn(),
  putSetupClient: vi.fn(),
  putGithubPat: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

const state = (g: boolean, a: boolean, installUrl?: string) => ({
  github: {
    configured: g,
    callbackUrl: 'http://127.0.0.1:4317/oauth/github/callback',
    ...(installUrl ? { installUrl } : {}),
  },
  atlassian: { configured: a, callbackUrl: 'http://127.0.0.1:4317/oauth/atlassian/callback' },
});

beforeEach(() => {
  Object.values(api).forEach((m) => m.mockReset());
  api.getSetup.mockResolvedValue(state(false, true));
});

describe('Setup page', () => {
  it('shows both callback URLs and which apps are configured', async () => {
    renderWithClient(<Setup />);
    expect(
      await screen.findByText('http://127.0.0.1:4317/oauth/github/callback'),
    ).toBeInTheDocument();
    expect(screen.getByText('http://127.0.0.1:4317/oauth/atlassian/callback')).toBeInTheDocument();
    expect(screen.getByText('Not configured')).toBeInTheDocument();
    expect(screen.getByText('Client credentials saved')).toBeInTheDocument();
  });

  it('offers a Connect link only for a configured app', async () => {
    renderWithClient(<Setup />);
    expect(await screen.findByRole('link', { name: 'Connect Jira' })).toHaveAttribute(
      'href',
      '/oauth/atlassian/start',
    );
    expect(screen.queryByRole('link', { name: 'Connect GitHub' })).toBeNull();
  });

  it('saves client credentials and clears the secret field', async () => {
    api.putSetupClient.mockResolvedValue(undefined);
    renderWithClient(<Setup />);
    const github = await screen.findByRole('region', { name: 'GitHub App' });
    const id = github.querySelector<HTMLInputElement>('input[name=clientId]')!;
    const secret = github.querySelector<HTMLInputElement>('input[name=clientSecret]')!;
    expect(secret.type).toBe('password');
    fireEvent.change(id, { target: { value: 'my-id' } });
    fireEvent.change(secret, { target: { value: 'my-secret' } });
    fireEvent.submit(github.querySelector('form')!);
    await waitFor(() =>
      expect(api.putSetupClient).toHaveBeenCalledWith('github', {
        clientId: 'my-id',
        clientSecret: 'my-secret',
      }),
    );
    await waitFor(() => expect(secret.value).toBe(''));
    expect(await screen.findByText('Saved')).toBeInTheDocument();
  });

  it('sends the optional app slug for GitHub only', async () => {
    api.putSetupClient.mockResolvedValue(undefined);
    renderWithClient(<Setup />);
    const github = await screen.findByRole('region', { name: 'GitHub App' });
    expect(screen.getByText(/github\.com\/apps\/<slug>/)).toBeInTheDocument();
    fireEvent.change(github.querySelector('input[name=clientId]')!, { target: { value: 'i' } });
    fireEvent.change(github.querySelector('input[name=clientSecret]')!, { target: { value: 's' } });
    fireEvent.change(github.querySelector('input[name=appSlug]')!, {
      target: { value: ' my-dash ' },
    });
    fireEvent.submit(github.querySelector('form')!);
    await waitFor(() =>
      expect(api.putSetupClient).toHaveBeenCalledWith('github', {
        clientId: 'i',
        clientSecret: 's',
        appSlug: 'my-dash',
      }),
    );
    const atlassian = screen.getByRole('region', { name: 'Atlassian OAuth 2.0 (3LO) app' });
    expect(atlassian.querySelector('input[name=appSlug]')).toBeNull();
  });

  it('shows the install link only for the validated server URL', async () => {
    api.getSetup.mockResolvedValue(
      state(true, true, 'https://github.com/apps/my-dash/installations/new'),
    );
    renderWithClient(<Setup />);
    const link = await screen.findByRole('link', { name: 'Install on repositories' });
    expect(link).toHaveAttribute('href', 'https://github.com/apps/my-dash/installations/new');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByText(/organization owner picks the repositories/)).toBeInTheDocument();
  });

  it('shows no install link without a slug or for an unexpected URL', async () => {
    api.getSetup.mockResolvedValue(state(true, true));
    const { unmount } = renderWithClient(<Setup />);
    await screen.findByRole('region', { name: 'GitHub App' });
    expect(screen.queryByRole('link', { name: 'Install on repositories' })).toBeNull();
    unmount();
    api.getSetup.mockResolvedValue(
      state(true, true, 'https://evil.example/apps/x/installations/new'),
    );
    renderWithClient(<Setup />);
    await screen.findByRole('region', { name: 'GitHub App' });
    expect(screen.queryByRole('link', { name: 'Install on repositories' })).toBeNull();
  });

  it('mentions fine-grained tokens limited to selected repositories', async () => {
    renderWithClient(<Setup />);
    expect(await screen.findByText(/limited to selected repositories/)).toBeInTheDocument();
  });

  it('validates a token and shows the login and the warning', async () => {
    api.putGithubPat.mockResolvedValue({
      connected: true,
      login: 'octocat',
      method: 'pat',
      warning: 'Fine-grained tokens only see the repositories they were granted.',
    });
    renderWithClient(<Setup />);
    const pat = await screen.findByRole('region', { name: 'GitHub access token' });
    fireEvent.change(pat.querySelector('input')!, { target: { value: 'github_pat_x' } });
    fireEvent.submit(pat.querySelector('form')!);
    expect(await screen.findByText('Connected as octocat')).toBeInTheDocument();
    expect(
      screen.getAllByText(/Fine-grained tokens only see/).some((el) => el.tagName === 'P'),
    ).toBe(true);
    expect(api.putGithubPat).toHaveBeenCalledWith('github_pat_x');
  });

  it('explains a rejected token without showing the code', async () => {
    api.putGithubPat.mockRejectedValue(new ApiError(422, 'insufficient_scopes'));
    renderWithClient(<Setup />);
    const pat = await screen.findByRole('region', { name: 'GitHub access token' });
    fireEvent.change(pat.querySelector('input')!, { target: { value: 'ghp_x' } });
    fireEvent.submit(pat.querySelector('form')!);
    expect(await screen.findByRole('alert')).toHaveTextContent(/repo and read:org/);
    expect(screen.queryByText(/insufficient_scopes/)).toBeNull();
  });

  it('asks to open the app with the start script on 401', async () => {
    api.getSetup.mockRejectedValue(new ApiError(401, 'unauthenticated'));
    renderWithClient(<Setup />);
    expect(await screen.findByText('Open the app with scripts/start.sh')).toBeInTheDocument();
  });

  it('shows an error when setup cannot be loaded', async () => {
    api.getSetup.mockRejectedValue(new TypeError('network'));
    renderWithClient(<Setup />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not load the setup state.');
  });
});
