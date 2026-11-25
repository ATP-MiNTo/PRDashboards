import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { SidePanel } from './SidePanel';

function panel(open: boolean, onClose = vi.fn(), closeLabel?: string) {
  return (
    <SidePanel open={open} onClose={onClose} title="Account" closeLabel={closeLabel}>
      <button type="button">First</button>
      <button type="button">Last</button>
    </SidePanel>
  );
}

describe('SidePanel', () => {
  it('renders nothing while closed', () => {
    render(panel(false));
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('is a modal dialog named by its title, with a heading and a close button', () => {
    render(panel(true));
    const dialog = screen.getByRole('dialog', { name: 'Account' });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByRole('heading', { name: 'Account' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Close account' })).toBeInTheDocument();
  });

  it('takes the accessible name of the close button from closeLabel', () => {
    render(panel(true, vi.fn(), 'Dismiss the panel'));
    expect(screen.getByRole('button', { name: 'Dismiss the panel' })).toBeInTheDocument();
  });

  it('closes on Escape, on the scrim and on the close button', () => {
    const onClose = vi.fn();
    render(panel(true, onClose));
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    fireEvent.click(screen.getByTestId('side-panel-scrim'));
    fireEvent.click(screen.getByRole('button', { name: 'Close account' }));
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it('moves focus into the dialog and gives it back on close', () => {
    const opener = document.createElement('button');
    document.body.append(opener);
    opener.focus();
    const { rerender } = render(panel(true));
    expect(screen.getByRole('dialog')).toHaveFocus();
    rerender(panel(false));
    expect(opener).toHaveFocus();
    opener.remove();
  });

  it('keeps Tab inside the dialog', () => {
    render(panel(true));
    const dialog = screen.getByRole('dialog');
    const first = screen.getByRole('button', { name: 'Close account' });
    const last = screen.getByRole('button', { name: 'Last' });
    last.focus();
    fireEvent.keyDown(last, { key: 'Tab' });
    expect(first).toHaveFocus();
    fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
    dialog.focus();
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
    last.focus();
    fireEvent.keyDown(last, { key: 'a' });
    expect(last).toHaveFocus();
  });
});
