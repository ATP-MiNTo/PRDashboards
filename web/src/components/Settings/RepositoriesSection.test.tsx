import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { RepoSettings } from '@/api/client';
import { pr, snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';

const api = vi.hoisted(() => ({
  getRepos: vi.fn(),
  putRepo: vi.fn(),
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
  });
  api.putSettings.mockImplementation((s: unknown) => Promise.resolve(s));
});

describe('RepositoriesSection', () => {
  it('lists repositories collapsed, and opens a new one with its badge and note', async () => {
    renderWithClient(<RepositoriesSection />);
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
    renderWithClient(<RepositoriesSection />);
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
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('combobox', { name: 'Default for new repositories' }));
    fireEvent.click(screen.getByRole('option', { name: 'Rebase' }));
    await waitFor(() => expect(api.putSettings).toHaveBeenCalled());
    expect(api.putSettings.mock.calls[0]?.[0]).toMatchObject({
      defaultMethodForNewRepos: 'rebase',
    });
  });

  it('edits the ordered rules: add, change, reorder and remove, then saves them in one request', async () => {
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    const list = screen.getByRole('list', { name: 'Branch rules of acme/web' });
    expect(
      within(list)
        .getAllByRole('combobox', { name: /Branch pattern/ })
        .map((i) => (i as HTMLInputElement).value),
    ).toEqual(['main', 'production']);
    fireEvent.click(screen.getByRole('button', { name: 'Add rule to acme/web' }));
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
    fireEvent.click(screen.getByRole('button', { name: 'Save acme/web' }));
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
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    const input = screen.getByRole('combobox', { name: 'Branch pattern 1 of acme/web' });
    const options = [
      ...document.getElementById(input.getAttribute('list')!)!.querySelectorAll('option'),
    ];
    expect(options.map((o) => o.value)).toEqual(['feat/x', 'feat/y', 'main', 'release/1']);
  });

  it('rejects a bad pattern and keeps Save disabled', async () => {
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    const save = screen.getByRole('button', { name: 'Save acme/web' });
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
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('button', { name: /^acme\/web/ }));
    expect(screen.getByRole('button', { name: 'Add rule to acme/web' })).toBeDisabled();
  });

  it('lets a new repository be confirmed without changes', async () => {
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Save acme/api' }));
    await waitFor(() =>
      expect(api.putRepo).toHaveBeenCalledWith('R2', expect.objectContaining({ clearNew: true })),
    );
  });

  it('shows an error when the save fails', async () => {
    api.putRepo.mockRejectedValue(new Error('x'));
    renderWithClient(<RepositoriesSection />);
    fireEvent.click(await screen.findByRole('button', { name: 'Save acme/api' }));
    expect(await screen.findByText('Could not save the repository.')).toBeInTheDocument();
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
