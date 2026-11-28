import { act, fireEvent, render, screen } from '@testing-library/react';
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

const live = () => screen.getByRole('status');

describe('toasts', () => {
  it('keeps one polite live region mounted, and shows a message in it for 3.2 seconds', () => {
    render(
      <ToastProvider>
        <Button message="Refreshed" />
      </ToastProvider>,
    );
    expect(TOAST_MS).toBe(3200);
    const region = live();
    expect(region).toHaveAttribute('aria-live', 'polite');
    expect(region).toBeEmptyDOMElement();
    press('show Refreshed');
    expect(live()).toBe(region);
    expect(region).toHaveTextContent('Refreshed');
    act(() => vi.advanceTimersByTime(TOAST_MS - 1));
    expect(region).toHaveTextContent('Refreshed');
    act(() => vi.advanceTimersByTime(1));
    expect(region).toBeEmptyDOMElement();
    expect(live()).toBe(region);
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
    expect([...live().children].map((s) => s.textContent)).toEqual(['One', 'Two']);
    act(() => vi.advanceTimersByTime(TOAST_MS - 1000));
    expect([...live().children].map((s) => s.textContent)).toEqual(['Two']);
    act(() => vi.advanceTimersByTime(1000));
    expect(live()).toBeEmptyDOMElement();
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
    const classes = [...live().children, screen.getByRole('alert')].map((s) => s.className);
    expect(new Set(classes).size).toBe(3);
  });

  it('shows an error as an alert outside the status region, until it is dismissed', () => {
    render(
      <ToastProvider>
        <Button message="Merge failed" kind="error" />
      </ToastProvider>,
    );
    press('show Merge failed');
    const alert = screen.getByRole('alert');
    expect(alert).toHaveTextContent('Merge failed');
    expect(live()).not.toContainElement(alert);
    act(() => vi.advanceTimersByTime(TOAST_MS * 10));
    expect(screen.getByRole('alert')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss' }));
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('pauses while the pointer is over a toast and goes on with the time left', () => {
    render(
      <ToastProvider>
        <Button message="Hold" />
      </ToastProvider>,
    );
    press('show Hold');
    const toast = live().firstElementChild!;
    act(() => vi.advanceTimersByTime(2000));
    fireEvent.mouseEnter(toast);
    act(() => vi.advanceTimersByTime(60_000));
    expect(live()).toHaveTextContent('Hold');
    fireEvent.mouseLeave(toast);
    act(() => vi.advanceTimersByTime(TOAST_MS - 2000 - 1));
    expect(live()).toHaveTextContent('Hold');
    act(() => vi.advanceTimersByTime(1));
    expect(live()).toBeEmptyDOMElement();
  });

  it('pauses while the focus is inside a toast and resumes when it leaves', () => {
    render(
      <ToastProvider>
        <Button message="Focus" />
      </ToastProvider>,
    );
    press('show Focus');
    const toast = live().firstElementChild!;
    fireEvent.focus(toast);
    act(() => vi.advanceTimersByTime(60_000));
    expect(live()).toHaveTextContent('Focus');
    fireEvent.blur(toast);
    act(() => vi.advanceTimersByTime(TOAST_MS));
    expect(live()).toBeEmptyDOMElement();
  });

  it('does nothing without a provider', () => {
    render(<Button message="Lost" />);
    press('show Lost');
    expect(screen.queryByRole('status')).toBeNull();
    expect(screen.queryByRole('alert')).toBeNull();
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
