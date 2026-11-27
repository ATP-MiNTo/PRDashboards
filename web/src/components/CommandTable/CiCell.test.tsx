import { fireEvent, render, screen, within } from '@testing-library/react';
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

const open = (number = 1) =>
  fireEvent.click(screen.getByRole('button', { name: `Checks for #${number}` }));

describe('CiCell badge', () => {
  it.each([
    ['passing', 'Ready'],
    ['failing', 'Fails'],
    ['running', 'Running'],
    ['none', 'No CI'],
  ] as const)('says %s as "%s"', (state, label) => {
    render(<CiCell pr={pr('a', 1, { ci: { state, total: 0, checks: [] } })} />);
    expect(screen.getByText(label)).toBeInTheDocument();
  });

  it('has a spinner only while running', () => {
    const { container, rerender } = render(
      <CiCell
        pr={pr('a', 1, {
          ci: { state: 'running', total: 1, checks: [check({ state: 'running' })] },
        })}
      />,
    );
    expect(container.querySelector('svg')).not.toBeNull();
    rerender(
      <CiCell pr={pr('a', 1, { ci: { state: 'passing', total: 1, checks: [check({})] } })} />,
    );
    expect(container.querySelector('svg')).toBeNull();
  });

  it('keeps the popover closed until the "i" is used', () => {
    render(<CiCell pr={pr('a', 1, { ci: { state: 'passing', total: 1, checks: [check({})] } })} />);
    const info = screen.getByRole('button', { name: 'Checks for #1' });
    expect(info).toHaveAttribute('aria-haspopup', 'dialog');
    expect(info).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});

describe('CiCell popover', () => {
  it('lists each check with a name and state, and links to all checks on GitHub', () => {
    render(
      <CiCell
        pr={pr('a', 7, {
          ci: {
            state: 'failing',
            total: 3,
            checks: [
              check({ name: 'unit' }),
              check({ name: 'lint', state: 'failing' }),
              check({ name: 'build', state: 'pending' }),
            ],
          },
        })}
      />,
    );
    open(7);
    const dialog = screen.getByRole('dialog', { name: 'Checks for acme/web #7' });
    expect(within(dialog).getByText('Checks for acme/web #7')).toBeInTheDocument();
    const rows = within(within(dialog).getByRole('list', { name: 'Checks' })).getAllByRole(
      'listitem',
    );
    expect(rows.map((r) => r.textContent)).toEqual(['unitpassed', 'lintfailed', 'buildqueued']);
    const link = within(dialog).getByRole('link', { name: 'Open all checks on GitHub ↗' });
    expect(link).toHaveAttribute('href', 'https://github.com/acme/web/pull/7/checks');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(screen.getByRole('button', { name: 'Checks for #7' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
  });

  it('moves the focus into the popover, and Escape closes it and gives the focus back', () => {
    render(<CiCell pr={pr('a', 1, { ci: { state: 'passing', total: 1, checks: [check({})] } })} />);
    open();
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveFocus();
    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.getByRole('button', { name: 'Checks for #1' })).toHaveFocus();
  });

  it('closes on a click outside and toggles with the "i"', () => {
    render(<CiCell pr={pr('a', 1, { ci: { state: 'passing', total: 1, checks: [check({})] } })} />);
    open();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('dialog')).toBeNull();
    open();
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    open();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('closes on Tab so the focus is not left in a floating panel', () => {
    render(<CiCell pr={pr('a', 1, { ci: { state: 'passing', total: 1, checks: [check({})] } })} />);
    open();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Tab' });
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('says "+N more" when GitHub reports more checks than the list holds', () => {
    render(
      <CiCell pr={pr('a', 7, { ci: { state: 'passing', total: 62, checks: [check({})] } })} />,
    );
    open(7);
    expect(screen.getByText('+61 more')).toBeInTheDocument();
  });

  it('says so when the names of the checks are not available or there are none', () => {
    const { unmount } = render(
      <CiCell pr={pr('a', 7, { ci: { state: 'passing', total: 3, checks: [] } })} />,
    );
    open(7);
    expect(screen.getByText('3 checks, their names are not available')).toBeInTheDocument();
    expect(screen.queryByRole('list')).toBeNull();
    unmount();
    render(<CiCell pr={pr('a', 7, { ci: { state: 'none', total: 0, checks: [] } })} />);
    open(7);
    expect(screen.getByText('No checks reported')).toBeInTheDocument();
  });

  it('leaves out the GitHub link when the repository name cannot form one', () => {
    const row = pr('a', 1, { ci: { state: 'passing', total: 1, checks: [check({})] } });
    render(<CiCell pr={{ ...row, repo: { ...row.repo, nameWithOwner: 'bad name' } }} />);
    open();
    expect(screen.queryByRole('link')).toBeNull();
  });
});
