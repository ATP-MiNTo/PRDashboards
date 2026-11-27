import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { TOAST_MS, ToastProvider, useToast, type ToastKind } from './Toast';

function Button({ message, kind }: { message: string; kind?: ToastKind }) {
  const toast = useToast();
  return (
    <button type="button" onClick={() => toast.show(message, kind)}>
      show {message}
    </button>
  );
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const press = (name: string) =>
  act(() => {
    screen.getByRole('button', { name }).click();
  });

describe('toasts', () => {
  it('shows a message as a status and removes it after 3.2 seconds', () => {
    render(
      <ToastProvider>
        <Button message="Refreshed" />
      </ToastProvider>,
    );
    expect(TOAST_MS).toBe(3200);
    expect(screen.queryByRole('status')).toBeNull();
    press('show Refreshed');
    expect(screen.getByRole('status')).toHaveTextContent('Refreshed');
    act(() => vi.advanceTimersByTime(TOAST_MS - 1));
    expect(screen.getByRole('status')).toBeInTheDocument();
    act(() => vi.advanceTimersByTime(1));
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('stacks several toasts, each leaving after its own time', () => {
    render(
      <ToastProvider>
        <Button message="One" />
        <Button message="Two" kind="success" />
      </ToastProvider>,
    );
    press('show One');
    act(() => vi.advanceTimersByTime(1000));
    press('show Two');
    expect(screen.getAllByRole('status').map((s) => s.textContent)).toEqual(['One', 'Two']);
    act(() => vi.advanceTimersByTime(TOAST_MS - 1000));
    expect(screen.getAllByRole('status').map((s) => s.textContent)).toEqual(['Two']);
    act(() => vi.advanceTimersByTime(1000));
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('gives each kind its own class and defaults to info', () => {
    render(
      <ToastProvider>
        <Button message="a" />
        <Button message="b" kind="success" />
        <Button message="c" kind="error" />
      </ToastProvider>,
    );
    press('show a');
    press('show b');
    press('show c');
    const classes = screen.getAllByRole('status').map((s) => s.className);
    expect(new Set(classes).size).toBe(3);
  });

  it('does nothing without a provider', () => {
    render(<Button message="Lost" />);
    press('show Lost');
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('clears its timers when it unmounts', () => {
    const { unmount } = render(
      <ToastProvider>
        <Button message="Bye" />
      </ToastProvider>,
    );
    press('show Bye');
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
