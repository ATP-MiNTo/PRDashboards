import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { pr } from '@/test-fixtures';
import { MethodCell } from './MethodCell';

const base = pr('a', 1).mergeMethod;

describe('MethodCell', () => {
  it('labels the resolved method as the default and shows where it came from', () => {
    render(
      <MethodCell
        resolution={{ ...base, source: 'rule: release/*' }}
        value="squash"
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByRole('combobox', { name: 'Merge method' })).toHaveTextContent(
      'Squash (default)',
    );
    expect(screen.getByText('Branch rule release/*')).toBeInTheDocument();
  });

  it('disables a method the repository does not allow', () => {
    const onChange = vi.fn();
    render(
      <MethodCell
        resolution={{ ...base, allowed: ['squash', 'merge'] }}
        value="squash"
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByRole('combobox'));
    const rebase = screen.getByRole('option', { name: 'Rebase (not allowed)' });
    expect(rebase).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(rebase);
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('option', { name: 'Merge commit' }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith('merge');
  });

  it('shows the fallback warning', () => {
    render(
      <MethodCell
        resolution={{
          ...base,
          method: 'merge',
          default: 'squash',
          allowed: ['merge'],
          warning: 'Repo disallows squash merge, using merge commit',
        }}
        value="merge"
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText('Repo disallows squash merge, using merge commit')).toBeInTheDocument();
    expect(screen.getByRole('combobox')).toHaveTextContent('Merge commit (default)');
  });

  it('words the other sources', () => {
    const { rerender } = render(
      <MethodCell
        resolution={{ ...base, source: 'new repo default' }}
        value="squash"
        onChange={vi.fn()}
      />,
    );
    expect(screen.getByText('New repo default')).toBeInTheDocument();
    rerender(<MethodCell resolution={base} value="squash" onChange={vi.fn()} />);
    expect(screen.getByText('Repo default')).toBeInTheDocument();
  });
});
