import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Dropdown, type DropdownOption } from './Dropdown';

const OPTIONS: DropdownOption<'squash' | 'merge' | 'rebase'>[] = [
  { value: 'squash', label: 'Squash (default)' },
  { value: 'merge', label: 'Merge commit' },
  { value: 'rebase', label: 'Rebase (not allowed)', disabled: true },
];

function setup(over: Partial<React.ComponentProps<typeof Dropdown>> = {}) {
  const onChange = vi.fn();
  render(
    <Dropdown
      label="Merge method"
      value="squash"
      options={OPTIONS}
      onChange={onChange}
      {...over}
    />,
  );
  return { onChange, box: screen.getByRole('combobox', { name: 'Merge method' }) };
}

const press = (el: HTMLElement, key: string) => fireEvent.keyDown(el, { key });

afterEach(() => vi.useRealTimers());

describe('Dropdown', () => {
  it('shows the selected label and opens a listbox on click', () => {
    const { box } = setup();
    expect(box).toHaveTextContent('Squash (default)');
    expect(box).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(box);
    expect(box).toHaveAttribute('aria-expanded', 'true');
    const list = screen.getByRole('listbox', { name: 'Merge method' });
    expect(box).toHaveAttribute('aria-controls', list.id);
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(
      OPTIONS.map((o) => o.label),
    );
    expect(screen.getByRole('option', { name: 'Squash (default)' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
    expect(screen.getByRole('option', { name: 'Rebase (not allowed)' })).toHaveAttribute(
      'aria-disabled',
      'true',
    );
  });

  it('selects an option by click and ignores a disabled one', () => {
    const { onChange, box } = setup();
    fireEvent.click(box);
    fireEvent.click(screen.getByRole('option', { name: 'Rebase (not allowed)' }));
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('option', { name: 'Merge commit' }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith('merge');
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('does not report the value that is already selected', () => {
    const { onChange, box } = setup();
    fireEvent.click(box);
    fireEvent.click(screen.getByRole('option', { name: 'Squash (default)' }));
    expect(onChange).not.toHaveBeenCalled();
  });

  it('opens on hover after a delay and closes after a delay', () => {
    vi.useFakeTimers();
    const { box } = setup();
    const root = box.parentElement as HTMLElement;
    fireEvent.mouseEnter(root);
    expect(screen.queryByRole('listbox')).toBeNull();
    act(() => void vi.advanceTimersByTime(150));
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    fireEvent.mouseLeave(root);
    act(() => void vi.advanceTimersByTime(100));
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    act(() => void vi.advanceTimersByTime(200));
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('stays open when the pointer moves onto the list', () => {
    vi.useFakeTimers();
    const { box } = setup();
    const root = box.parentElement as HTMLElement;
    fireEvent.mouseEnter(root);
    act(() => void vi.advanceTimersByTime(150));
    fireEvent.mouseLeave(root);
    fireEvent.mouseEnter(screen.getByRole('listbox'));
    act(() => void vi.advanceTimersByTime(1000));
    expect(screen.getByRole('listbox')).toBeInTheDocument();
  });

  it('keeps the list open when a click follows the hover that opened it', () => {
    vi.useFakeTimers();
    const { box } = setup();
    const root = box.parentElement as HTMLElement;
    fireEvent.mouseEnter(root);
    act(() => void vi.advanceTimersByTime(150));
    fireEvent.click(box);
    fireEvent.mouseLeave(root);
    act(() => void vi.advanceTimersByTime(1000));
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    fireEvent.click(box);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('does not open on hover when disabled', () => {
    vi.useFakeTimers();
    const { box } = setup({ disabled: true });
    fireEvent.mouseEnter(box.parentElement as HTMLElement);
    act(() => void vi.advanceTimersByTime(500));
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('moves with the arrow keys, skips disabled options and picks with Enter', () => {
    const { onChange, box } = setup();
    box.focus();
    const active = () => box.getAttribute('aria-activedescendant');
    const id = (name: string) => screen.getByRole('option', { name }).id;
    press(box, 'ArrowDown');
    expect(active()).toBe(id('Squash (default)'));
    press(box, 'ArrowDown');
    expect(active()).toBe(id('Merge commit'));
    press(box, 'ArrowDown');
    expect(active()).toBe(id('Merge commit'));
    press(box, 'ArrowUp');
    press(box, 'ArrowUp');
    expect(active()).toBe(id('Squash (default)'));
    press(box, 'End');
    expect(active()).toBe(id('Merge commit'));
    press(box, 'Home');
    expect(active()).toBe(id('Squash (default)'));
    press(box, 'ArrowDown');
    press(box, 'Enter');
    expect(onChange).toHaveBeenCalledExactlyOnceWith('merge');
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(box).toHaveFocus();
  });

  it('opens with Enter or Space and closes with Escape without leaking it', () => {
    const outer = vi.fn();
    const { box } = setup();
    document.body.addEventListener('keydown', outer);
    box.focus();
    press(box, 'Enter');
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    press(box, 'Escape');
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(outer).not.toHaveBeenCalledWith(expect.objectContaining({ key: 'Escape' }));
    press(box, ' ');
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    press(box, 'Tab');
    expect(screen.queryByRole('listbox')).toBeNull();
    press(box, 'Escape');
    expect(outer).toHaveBeenCalledWith(expect.objectContaining({ key: 'Escape' }));
    document.body.removeEventListener('keydown', outer);
  });

  it('jumps to a typed prefix and resets the buffer after a pause', () => {
    vi.useFakeTimers();
    const { box } = setup();
    box.focus();
    const active = () => box.getAttribute('aria-activedescendant');
    const id = (name: string) => screen.getByRole('option', { name }).id;
    press(box, 'm');
    expect(active()).toBe(id('Merge commit'));
    press(box, 'x');
    expect(active()).toBe(id('Merge commit'));
    vi.setSystemTime(Date.now() + 1000);
    press(box, 's');
    expect(active()).toBe(id('Squash (default)'));
  });

  it('never selects a disabled option by typing', () => {
    const { onChange, box } = setup();
    box.focus();
    press(box, 'r');
    press(box, 'Enter');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('closes on blur', () => {
    vi.useFakeTimers();
    const { box } = setup();
    fireEvent.click(box);
    fireEvent.blur(box);
    act(() => void vi.advanceTimersByTime(1));
    expect(screen.queryByRole('listbox')).toBeNull();
  });
});
