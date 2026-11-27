import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { MenuItem, MenuNote, MenuTitle, PopMenu } from './PopMenu';

function Menu({
  onPick = vi.fn(),
  hover = false,
  disabled = false,
  onOpen,
}: {
  onPick?: (n: string) => void;
  hover?: boolean;
  disabled?: boolean;
  onOpen?: () => void;
}) {
  return (
    <PopMenu
      label="Choose"
      menuLabel="Things"
      triggerClassName={undefined}
      trigger="Open"
      width="w44"
      hover={hover}
      disabled={disabled}
      onOpen={onOpen}
    >
      {(close) => (
        <>
          <MenuTitle>Pick one</MenuTitle>
          <MenuItem
            onSelect={() => {
              close();
              onPick('a');
            }}
          >
            Alpha
          </MenuItem>
          <MenuItem disabled onSelect={() => onPick('b')}>
            Beta
          </MenuItem>
          <MenuItem active onSelect={() => onPick('c')}>
            Gamma
          </MenuItem>
          <MenuNote>Nothing else</MenuNote>
        </>
      )}
    </PopMenu>
  );
}

const trigger = () => screen.getByRole('button', { name: 'Choose' });

afterEach(() => vi.useRealTimers());

describe('PopMenu', () => {
  it('opens a labelled menu on click and puts the focus on the first item', () => {
    const onOpen = vi.fn();
    render(<Menu onOpen={onOpen} />);
    expect(trigger()).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(trigger());
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(trigger()).toHaveAttribute('aria-expanded', 'true');
    const menu = screen.getByRole('menu', { name: 'Things' });
    expect(menu).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Alpha' })).toHaveFocus();
    expect(screen.getByText('Pick one')).toBeInTheDocument();
    expect(screen.getByText('Nothing else')).toBeInTheDocument();
  });

  it('selects an item and closes', () => {
    const onPick = vi.fn();
    render(<Menu onPick={onPick} />);
    fireEvent.click(trigger());
    fireEvent.click(screen.getByRole('menuitem', { name: 'Alpha' }));
    expect(onPick).toHaveBeenCalledWith('a');
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('does not select a disabled item, and skips it with the arrow keys', () => {
    const onPick = vi.fn();
    render(<Menu onPick={onPick} />);
    fireEvent.click(trigger());
    const beta = screen.getByRole('menuitem', { name: 'Beta' });
    expect(beta).toHaveAttribute('aria-disabled', 'true');
    fireEvent.click(beta);
    expect(onPick).not.toHaveBeenCalled();
    const menu = screen.getByRole('menu').parentElement!;
    fireEvent.keyDown(menu, { key: 'ArrowDown' });
    expect(screen.getByRole('menuitem', { name: 'Gamma' })).toHaveFocus();
    fireEvent.keyDown(menu, { key: 'ArrowDown' });
    expect(screen.getByRole('menuitem', { name: 'Alpha' })).toHaveFocus();
    fireEvent.keyDown(menu, { key: 'ArrowUp' });
    expect(screen.getByRole('menuitem', { name: 'Gamma' })).toHaveFocus();
  });

  it('opens with the arrow keys from the button', () => {
    render(<Menu />);
    fireEvent.keyDown(trigger(), { key: 'ArrowDown' });
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });

  it('closes with Escape and gives the focus back to the button', () => {
    render(<Menu />);
    fireEvent.click(trigger());
    fireEvent.keyDown(screen.getByRole('menuitem', { name: 'Alpha' }), { key: 'Escape' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(trigger()).toHaveFocus();
  });

  it('closes on a click outside, and on Tab', () => {
    render(<Menu />);
    fireEvent.click(trigger());
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('menu')).toBeNull();
    fireEvent.click(trigger());
    fireEvent.keyDown(screen.getByRole('menuitem', { name: 'Alpha' }), { key: 'Tab' });
    expect(screen.queryByRole('menu')).toBeNull();
    expect(trigger()).toHaveFocus();
  });

  it('toggles with the button', () => {
    render(<Menu />);
    fireEvent.click(trigger());
    fireEvent.click(trigger());
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('does not open when disabled', () => {
    render(<Menu disabled />);
    expect(trigger()).toBeDisabled();
    fireEvent.click(trigger());
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('also opens while the pointer rests on the button when asked, and leaves after it', () => {
    vi.useFakeTimers();
    render(<Menu hover />);
    fireEvent.mouseEnter(trigger().parentElement!);
    act(() => void vi.advanceTimersByTime(200));
    expect(screen.getByRole('menu')).toBeInTheDocument();
    fireEvent.mouseLeave(trigger().parentElement!);
    act(() => void vi.advanceTimersByTime(400));
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('stays open after a click while the pointer leaves', () => {
    vi.useFakeTimers();
    render(<Menu hover />);
    fireEvent.click(trigger());
    fireEvent.mouseLeave(trigger().parentElement!);
    act(() => void vi.advanceTimersByTime(400));
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
});
