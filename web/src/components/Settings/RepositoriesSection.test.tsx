import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { RepoSettings } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getRepos: vi.fn(),
  putRepo: vi.fn(),
  putRepos: vi.fn(),
  getSnapshot: vi.fn(),
  getSettings: vi.fn(),
  putSettings: vi.fn(),
}));

vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { branchesOf, RepositoriesSection, validPattern } from './RepositoriesSection';

const repo = (id: string, name: string, over: Partial<RepoSettings> = {}): RepoSettings => ({
  id,
  nameWithOwner: name,
  isNew: false,
  openPrs: 2,
  approvalsRequired: 1,
  defaultMethod: 'squash',
  branchRules: [
    { pattern: 'main', method: 'merge' },
    { pattern: 'production', method: 'merge' },
  ],
  ...over,
});

beforeEach(() => {
  api.getRepos.mockResolvedValue({
    repos: [repo('R1', 'acme/web'), repo('R2', 'acme/api', { isNew: true, openPrs: 1 })],
  });
  api.putRepo.mockImplementation((_id: string, u: unknown) => Promise.resolve(u));
  api.putRepos.mockImplementation(() => Promise.resolve({ repos: [] }));
  api.getSnapshot.mockResolvedValue(
    snapshot([
      pr('a', 1, { baseRef: 'main', headRef: 'feat/x' }),
      pr('b', 2, {
        baseRef: 'release/1',
        headRef: 'feat/y',
        repo: { ...pr('b', 2).repo, id: 'R1' },
      }),
      pr('c', 3, { baseRef: 'other', headRef: 'feat/z', repo: { ...pr('c', 3).repo, id: 'R9' } }),
    ]),
  );
  api.getSettings.mockResolvedValue({
    polling: { githubSec: 60, jiraSec: 300 },
    theme: 'auto',
    defaultMethodForNewRepos: 'squash',
    deleteBranchAfterMerge: false,
    jiraSiteId: null,
    excludedStatuses: ['Done'],
  });
  api.putSettings.mockImplementation((s: unknown) => Promise.resolve(s));
});

/** Renders the section and unfolds it. */
async function renderOpen() {
  renderWithClient(<RepositoriesSection />);
  fireEvent.click(await screen.findByRole('button', { name: /^Repositories setting/ }));
}

const group = (repoName: string) => within(screen.getByRole('group', { name: repoName }));

describe('RepositoriesSection', () => {
  it('is folded by default and hides the list and its controls', async () => {
    renderWithClient(<RepositoriesSection />);
    const fold = await screen.findByRole('button', { name: /^Repositories setting/ });
    expect(fold).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('button', { name: /^acme\/web/ })).toBeNull();
    expect(screen.queryByRole('searchbox')).toBeNull();
    expect(screen.queryByRole('combobox', { name: 'Default for new repositories' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Save all' })).toBeNull();
    expect(screen.getByRole('heading', { name: /^Repositories setting/ })).toContainElement(fold);
    fireEvent.click(fold);
    expect(fold).toHaveAttribute('aria-expanded', 'true');
    expect(await screen.findByRole('button', { name: /^acme\/web/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save all' })).toBeInTheDocument();
    fireEvent.click(fold);
    expect(screen.queryByRole('button', { name: /^acme\/web/ })).toBeNull();
  });

  it('lists repositories collapsed, and opens a new one with its badge and note', async () => {
    await renderOpen();
    const web = await screen.findByRole('button', { name: /^acme\/web/ });
    const api2 = screen.getByRole('button', { name: /^acme\/api/ });
    expect(web).toHaveAttribute('aria-expanded', 'false');
    expect(api2).toHaveAttribute('aria-expanded', 'true');
    expect(within(api2).getByText('New')).toBeInTheDocument();
    expect(screen.getByText(/New repository\. It starts from/)).toBeInTheDocument();
    fireEvent.click(web);
    expect(web).toHaveAttribute('aria-expanded', 'true');
  });

  it('filters by the search text', async () => {
    await renderOpen();
    await screen.findByRole('button', { name: /^acme\/web/ });
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search repositories' }), {
      target: { value: 'API' },
    });
    expect(screen.queryByRole('button', { name: /^acme\/web/ })).toBeNull();
    expect(screen.getByRole('button', { name: /^acme\/api/ })).toBeInTheDocument();
    fireEvent.change(screen.getByRole('searchbox'), { target: { value: 'zzz' } });
    expect(screen.getByText('No repository matches.')).toBeInTheDocument();
  });

  it('saves the default for new repositories', async () => {
    await renderOpen();
    fireEvent.click(await screen.findByRole('combobox', { name: 'Default for new repositories' }));
    fireEvent.click(screen.getByRole('option', { name: 'Rebase' }));
    await waitFor(() => expect(api.putSettings).toHaveBeenCalled());
    expect(api.putSettings.mock.calls[0]?.[0]).toMatchObject({
      defaultMethodForNewRepos: 'rebase',
    });
  });

  it('edits the ordered rules: add, change, reorder and remove, then saves them in one request', async () => {
    await renderOpen();
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    const list = screen.getByRole('list', { name: 'Branch rules of acme/web' });
    expect(
      within(list)
        .getAllByRole('combobox', { name: /Branch pattern/ })
        .map((i) => (i as HTMLInputElement).value),
    ).toEqual(['main', 'production']);
    fireEvent.click(group('acme/web').getByRole('button', { name: 'Add rule' }));
    fireEvent.change(screen.getByRole('combobox', { name: 'Branch pattern 3 of acme/web' }), {
      target: { value: 'release/*' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Move rule 3 of acme/web up' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove rule 1 of acme/web' }));
    expect(
      within(list)
        .getAllByRole('combobox', { name: /Branch pattern/ })
        .map((i) => (i as HTMLInputElement).value),
    ).toEqual(['release/*', 'production']);
    fireEvent.click(screen.getByRole('combobox', { name: 'Default method of acme/web' }));
    fireEvent.click(screen.getByRole('option', { name: 'Merge commit' }));
    fireEvent.click(group('acme/web').getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(api.putRepo).toHaveBeenCalledTimes(1));
    expect(api.putRepo).toHaveBeenCalledWith('R1', {
      defaultMethod: 'merge',
      branchRules: [
        { pattern: 'release/*', method: 'squash' },
        { pattern: 'production', method: 'merge' },
      ],
      clearNew: true,
    });
  });

  it('feeds the pattern combobox from the branches of that repository only', async () => {
    await renderOpen();
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    const input = screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' });
    const options = [
      ...document.getElementById(input.getAttribute('list')!)!.querySelectorAll('option'),
    ];
    expect(options.map((o) => o.value)).toEqual(['feat/x', 'feat/y', 'main', 'release/1']);
  });

  it('rejects a bad pattern and keeps Save disabled', async () => {
    await renderOpen();
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    const save = group('acme/web').getByRole('button', { name: 'Save' });
    expect(save).toBeDisabled();
    fireEvent.change(screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' }), {
      target: { value: 'a b(' },
    });
    expect(screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' })).toHaveAttribute(
      'aria-invalid',
      'true',
    );
    expect(screen.getByRole('alert')).toHaveTextContent('Patterns use letters');
    expect(save).toBeDisabled();
  });

  it('stops at 50 rules', async () => {
    api.getRepos.mockResolvedValue({
      repos: [
        repo('R1', 'acme/web', {
          branchRules: Array.from({ length: 50 }, (_, i) => ({
            pattern: `b${i}`,
            method: 'merge' as const,
          })),
        }),
      ],
    });
    await renderOpen();
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    expect(group('acme/web').getByRole('button', { name: 'Add rule' })).toBeDisabled();
  });

  it('lets a new repository be confirmed without changes', async () => {
    await renderOpen();
    fireEvent.click(await group('acme/api').findByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(api.putRepo).toHaveBeenCalledWith('R2', expect.objectContaining({ clearNew: true })),
    );
  });

  it('shows an error when the save fails', async () => {
    api.putRepo.mockRejectedValue(new Error('x'));
    await renderOpen();
    fireEvent.click(await group('acme/api').findByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Could not save the repository.')).toBeInTheDocument();
  });
});

describe('Save all', () => {
  const edit = async (name: string, pattern: string) => {
    const web = await screen.findByRole('button', { name: new RegExp(`^${name}`) });
    if (web.getAttribute('aria-expanded') === 'false') fireEvent.click(web);
    fireEvent.change(screen.getByRole('combobox', { name: `Branch pattern 1 of ${name}` }), {
      target: { value: pattern },
    });
  };

  it('is disabled while nothing is changed and no repository is new', async () => {
    api.getRepos.mockResolvedValue({ repos: [repo('R1', 'acme/web')] });
    await renderOpen();
    await screen.findByRole('button', { name: /^acme\/web/ });
    expect(screen.getByRole('button', { name: 'Save all' })).toBeDisabled();
  });

  it('saves every changed and every new repository in one request', async () => {
    api.getRepos.mockResolvedValue({
      repos: [
        repo('R1', 'acme/web'),
        repo('R2', 'acme/api', { isNew: true }),
        repo('R3', 'acme/docs'),
      ],
    });
    await renderOpen();
    await edit('acme/web', 'develop');
    const all = screen.getByRole('button', { name: 'Save all' });
    expect(all).toBeEnabled();
    fireEvent.click(all);
    await waitFor(() => expect(api.putRepos).toHaveBeenCalledTimes(1));
    expect(api.putRepo).not.toHaveBeenCalled();
    const { repos } = api.putRepos.mock.calls[0]![0] as {
      repos: {
        id: string;
        update: { defaultMethod: string; branchRules: unknown[]; clearNew: boolean };
      }[];
    };
    expect(repos.map((r) => r.id)).toEqual(['R1', 'R2']);
    expect(repos[0]!.update).toEqual({
      defaultMethod: 'squash',
      branchRules: [
        { pattern: 'develop', method: 'merge' },
        { pattern: 'production', method: 'merge' },
      ],
      clearNew: true,
    });
    expect(repos[1]!.update.clearNew).toBe(true);
  });

  it('drops the saved drafts and refetches the repositories', async () => {
    await renderOpen();
    await edit('acme/web', 'develop');
    fireEvent.click(screen.getByRole('button', { name: 'Save all' }));
    await waitFor(() => expect(api.putRepos).toHaveBeenCalled());
    await waitFor(() => expect(api.getRepos).toHaveBeenCalledTimes(2));
    expect(screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' })).toHaveValue(
      'main',
    );
  });

  it('is disabled while a changed draft is invalid', async () => {
    api.getRepos.mockResolvedValue({ repos: [repo('R1', 'acme/web')] });
    await renderOpen();
    await edit('acme/web', 'a b(');
    expect(screen.getByRole('button', { name: 'Save all' })).toBeDisabled();
    await edit('acme/web', 'main2');
    expect(screen.getByRole('button', { name: 'Save all' })).toBeEnabled();
  });

  it('is disabled while a save is pending', async () => {
    api.putRepos.mockReturnValue(new Promise(() => undefined));
    api.getRepos.mockResolvedValue({ repos: [repo('R1', 'acme/web')] });
    await renderOpen();
    await edit('acme/web', 'develop');
    fireEvent.click(screen.getByRole('button', { name: 'Save all' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save all' })).toBeDisabled());
    expect(group('acme/web').getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  it('keeps the drafts and says so when the request fails', async () => {
    api.putRepos.mockRejectedValue(new Error('x'));
    api.getRepos.mockResolvedValue({ repos: [repo('R1', 'acme/web')] });
    await renderOpen();
    await edit('acme/web', 'develop');
    fireEvent.click(screen.getByRole('button', { name: 'Save all' }));
    expect(await screen.findByText('Could not save the repositories.')).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' })).toHaveValue(
      'develop',
    );
  });

  it('keeps the drafts of one repository when another is saved on its own', async () => {
    await renderOpen();
    await edit('acme/web', 'develop');
    fireEvent.click(await group('acme/api').findByRole('button', { name: 'Save' }));
    await waitFor(() => expect(api.putRepo).toHaveBeenCalledWith('R2', expect.anything()));
    await waitFor(() => expect(api.getRepos).toHaveBeenCalledTimes(2));
    expect(screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' })).toHaveValue(
      'develop',
    );
  });
});

describe('pattern and branch helpers', () => {
  it('accepts names and globs only', () => {
    for (const ok of ['main', 'release/*', 'feat/a.b_c-1']) expect(validPattern(ok)).toBe(true);
    for (const bad of ['', 'a b', 'a(b)', 'x'.repeat(201)]) expect(validPattern(bad)).toBe(false);
  });

  it('collects sorted unique branches of one repository', () => {
    const s = snapshot([
      pr('a', 1, { baseRef: 'main', headRef: 'b' }),
      pr('b', 2, { baseRef: 'main', headRef: 'a' }),
    ]);
    expect(branchesOf(s, 'R1')).toEqual(['a', 'b', 'main']);
    expect(branchesOf(s, 'none')).toEqual([]);
  });
});
