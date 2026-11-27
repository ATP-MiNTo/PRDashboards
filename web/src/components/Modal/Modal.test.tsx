import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { Modal } from './Modal';

function Page({
  onClose = vi.fn(),
  dismissible = true,
}: {
  onClose?: () => void;
  dismissible?: boolean;
}) {
  return (
    <>
      <button type="button">Outside</button>
      <Modal labelledBy="t" onClose={onClose} dismissible={dismissible}>
        <h3 id="t">Title</h3>
        <button type="button">First</button>
        <button type="button">Last</button>
      </Modal>
    </>
  );
}

describe('Modal', () => {
  it('is a labelled modal dialog at the end of the body, with the focus inside', () => {
    render(<Page />);
    const dialog = screen.getByRole('dialog', { name: 'Title' });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveFocus();
    expect(document.body.lastElementChild?.contains(dialog)).toBe(true);
  });

  it('closes with Escape and with a click on the scrim', () => {
    const onClose = vi.fn();
    render(<Page onClose={onClose} />);
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByTestId('modal-scrim'));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it('ignores Escape and the scrim while it cannot be dismissed', () => {
    const onClose = vi.fn();
    render(<Page onClose={onClose} dismissible={false} />);
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    fireEvent.click(screen.getByTestId('modal-scrim'));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('keeps Tab and Shift+Tab inside', () => {
    render(<Page />);
    const first = screen.getByRole('button', { name: 'First' });
    const last = screen.getByRole('button', { name: 'Last' });
    last.focus();
    fireEvent.keyDown(last, { key: 'Tab' });
    expect(first).toHaveFocus();
    fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
  });

  it('gives the focus back to where it was when it closes', () => {
    const { rerender } = render(<button type="button">Opener</button>);
    screen.getByRole('button', { name: 'Opener' }).focus();
    rerender(
      <>
        <button type="button">Opener</button>
        <Modal labelledBy="t" onClose={vi.fn()}>
          <h3 id="t">Title</h3>
        </Modal>
      </>,
    );
    expect(screen.getByRole('dialog')).toHaveFocus();
    rerender(<button type="button">Opener</button>);
    expect(screen.getByRole('button', { name: 'Opener' })).toHaveFocus();
  });
});
