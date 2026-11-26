import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { Group } from '@/lib/snapshot';
import { paginate } from '@/lib/view';
import { Pager } from './Pager';

const groups = (n: number): Group[] =>
  Array.from({ length: n }, (_, i) => ({ key: `p${i}`, ids: [`p${i}`], stacked: false }));

function setup(total: number, page = 0, size = 10) {
  const onPage = vi.fn();
  const onPageSize = vi.fn();
  render(
    <Pager
      pages={paginate(groups(total), size)}
      page={page}
      pageSize={size}
      onPage={onPage}
      onPageSize={onPageSize}
    />,
  );
  return { onPage, onPageSize };
}

describe('Pager', () => {
  it('renders nothing without pull requests', () => {
    const { container } = render(
      <Pager pages={[]} page={0} pageSize={10} onPage={vi.fn()} onPageSize={vi.fn()} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('keeps the page size select for a single page, without page buttons', () => {
    setup(4);
    expect(screen.getByText('Showing 1 to 4 of 4')).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Pull requests per page' })).toHaveValue('10');
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
  });

  it('offers 10, 15 and 20 per page', () => {
    const { onPageSize } = setup(30);
    const select = screen.getByRole('combobox', { name: 'Pull requests per page' });
    expect([...select.querySelectorAll('option')].map((o) => o.value)).toEqual(['10', '15', '20']);
    fireEvent.change(select, { target: { value: '20' } });
    expect(onPageSize).toHaveBeenCalledWith(20);
  });

  it('names the previous and next buttons and marks the current page', () => {
    const { onPage } = setup(25, 1);
    expect(screen.getByText('Showing 11 to 20 of 25')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Page 2' })).toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('button', { name: 'Page 1' })).not.toHaveAttribute('aria-current');
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Page 3' }));
    expect(onPage.mock.calls.map((c) => c[0])).toEqual([2, 0, 2]);
  });

  it('disables previous on the first page and next on the last', () => {
    setup(25, 0);
    expect(screen.getByRole('button', { name: 'Previous page' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Next page' })).toBeEnabled();
  });

  it('puts the page size select before the page buttons', () => {
    setup(25);
    const select = screen.getByRole('combobox');
    const prev = screen.getByRole('button', { name: 'Previous page' });
    expect(select.compareDocumentPosition(prev) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('skips pages in the middle of a long list', () => {
    setup(200, 9);
    const names = screen.getAllByRole('button').map((b) => b.getAttribute('aria-label'));
    expect(names).toContain('Page 20');
    expect(names).not.toContain('Page 5');
  });
});
