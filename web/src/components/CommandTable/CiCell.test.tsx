import { render, screen, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import type { Check } from '@/api/client';
import { pr } from '@/test-fixtures';
import { CiCell } from './CiCell';

const check = (over: Partial<Check>): Check => ({
  name: 'unit',
  app: 'GitHub Actions',
  state: 'passing',
  url: 'https://github.com/acme/web/actions/runs/1/job/2',
  startedAt: '2026-11-24T10:00:00Z',
  completedAt: '2026-11-24T10:02:30Z',
  ...over,
});

describe('CiCell', () => {
  it('shows the rollup first and each check with its status, app, duration and link', () => {
    render(
      <CiCell
        pr={pr('a', 1, {
          ci: {
            state: 'failing',
            total: 2,
            checks: [check({}), check({ name: 'lint', state: 'failing', app: null, url: null })],
          },
        })}
      />,
    );
    expect(screen.getByText('Failing')).toBeInTheDocument();
    const list = screen.getByRole('list', { name: 'Checks' });
    const [first, second] = within(list).getAllByRole('listitem');
    const link = within(first!).getByRole('link', { name: 'unit' });
    expect(link).toHaveAttribute('href', 'https://github.com/acme/web/actions/runs/1/job/2');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(within(first!).getByRole('img', { name: 'Passing' })).toBeInTheDocument();
    expect(within(first!).getByText('GitHub Actions')).toBeInTheDocument();
    expect(within(first!).getByText('2m 30s')).toBeInTheDocument();
    expect(within(second!).queryByRole('link')).toBeNull();
    expect(within(second!).getByRole('img', { name: 'Failing' })).toBeInTheDocument();
  });

  it('shows the host of an external CI and rejects links that are not https', () => {
    render(
      <CiCell
        pr={pr('a', 1, {
          ci: {
            state: 'passing',
            total: 3,
            checks: [
              check({ name: 'jenkins', app: null, url: 'https://ci.example.com/job/42' }),
              check({ name: 'plain', url: 'http://ci.example.com/1' }),
              check({ name: 'script', url: 'javascript:alert(1)' }),
            ],
          },
        })}
      />,
    );
    expect(screen.getByRole('link', { name: 'jenkins' })).toHaveAttribute(
      'href',
      'https://ci.example.com/job/42',
    );
    expect(screen.getByText('ci.example.com')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'plain' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'script' })).toBeNull();
    expect(screen.getAllByRole('link')).toHaveLength(1);
  });

  it('links "+N more" to the checks page when GitHub reports more than the list holds', () => {
    render(
      <CiCell pr={pr('a', 7, { ci: { state: 'passing', total: 62, checks: [check({})] } })} />,
    );
    const more = screen.getByRole('link', { name: '+61 more' });
    expect(more).toHaveAttribute('href', 'https://github.com/acme/web/pull/7/checks');
    expect(more).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('shows no list without checks, and no "+N more" when everything is listed', () => {
    render(<CiCell pr={pr('a', 1, { ci: { state: 'none', total: 0, checks: [] } })} />);
    expect(screen.getByText('No CI')).toBeInTheDocument();
    expect(screen.queryByRole('list')).toBeNull();
    expect(screen.queryByText(/more/)).toBeNull();
  });

  it('shows the overall state and the count when the check list is hidden', () => {
    render(<CiCell pr={pr('a', 7, { ci: { state: 'passing', total: 3, checks: [] } })} />);
    expect(screen.getByText('Passing')).toBeInTheDocument();
    expect(screen.queryByRole('list')).toBeNull();
    expect(screen.getByRole('link', { name: '3 checks' })).toHaveAttribute(
      'href',
      'https://github.com/acme/web/pull/7/checks',
    );
  });

  it('shows a plain "+N more" when the repository name cannot form a link', () => {
    const row = pr('a', 1, { ci: { state: 'passing', total: 3, checks: [check({})] } });
    render(<CiCell pr={{ ...row, repo: { ...row.repo, nameWithOwner: 'bad name' } }} />);
    expect(screen.getByText('+2 more').closest('a')).toBeNull();
  });
});
