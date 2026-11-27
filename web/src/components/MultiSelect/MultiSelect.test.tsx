import { fireEvent, render, screen, within } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { MultiSelect, type MultiSelectProps } from './MultiSelect';

const OPTIONS = ['Blocked', 'Closed', 'Done', 'In Review', 'Reopen', 'To Do'];

function setup(over: Partial<MultiSelectProps> = {}) {
  const onChange = vi.fn();
  const outer = vi.fn();
  render(
    <div onKeyDown={(e) => outer(e.key)}>
      <MultiSelect
        label="Exclude statuses"
        values={[]}
        options={OPTIONS}
        onChange={onChange}
        {...over}
      />
      <button type="button">elsewhere</button>
    </div>,
  );
  return { onChange, outer, box: screen.getByRole('combobox', { name: 'Exclude statuses' }) };
}

const press = (el: HTMLElement, key: string) => fireEvent.keyDown(el, { key });
const search = () => screen.getByRole('searchbox', { name: 'Search Exclude statuses' });

describe('MultiSelect', () => {
  it('calls onOpen each time the popover opens', () => {
    const onOpen = vi.fn();
    const { box } = setup({ onOpen });
    expect(onOpen).not.toHaveBeenCalled();
    fireEvent.click(box);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('summarises the picks on the trigger', () => {
    const { box } = setup();
    expect(box).toHaveTextContent('None');
    expect(box).toHaveAttribute('aria-haspopup', 'listbox');
    expect(box).toHaveAttribute('aria-expanded', 'false');
  });

  it('shows the first value and how many more', () => {
    const { box } = setup({ values: ['Done'] });
    expect(box).toHaveTextContent('Done');
    expect(box).not.toHaveTextContent('+');
    render(<MultiSelect label="Other" values={['A', 'B', 'C']} options={[]} onChange={vi.fn()} />);
    expect(screen.getByRole('combobox', { name: 'Other' })).toHaveTextContent('A +2');
  });

  it('opens on click only, not on hover, with a focused search and a multiselectable list', () => {
    const { box } = setup({ values: ['Done'] });
    fireEvent.mouseEnter(box);
    fireEvent.mouseOver(box);
    expect(screen.queryByRole('listbox')).toBeNull();
    fireEvent.click(box);
    expect(box).toHaveAttribute('aria-expanded', 'true');
    const list = screen.getByRole('listbox', { name: 'Exclude statuses' });
    expect(list).toHaveAttribute('aria-multiselectable', 'true');
    expect(box).toHaveAttribute('aria-controls', list.id);
    expect(search()).toHaveFocus();
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(
      OPTIONS.map((o) => (o === 'Done' ? '✓Done' : `✓${o}`)),
    );
    expect(screen.getByRole('option', { name: 'Done' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByRole('option', { name: 'Closed' })).toHaveAttribute(
      'aria-selected',
      'false',
    );
  });

  it('filters by case-insensitive substring and says when nothing matches', () => {
    const { box } = setup();
    fireEvent.click(box);
    fireEvent.change(search(), { target: { value: 'RE' } });
    expect(screen.getAllByRole('option').map((o) => o.textContent?.slice(1))).toEqual([
      'In Review',
      'Reopen',
    ]);
    expect(search()).toHaveAttribute('aria-activedescendant', screen.getAllByRole('option')[0]?.id);
    fireEvent.change(search(), { target: { value: 'zzz' } });
    expect(screen.queryAllByRole('option')).toHaveLength(0);
    expect(screen.getByText('No matching status')).toBeInTheDocument();
    expect(search()).not.toHaveAttribute('aria-activedescendant');
  });

  it('appends a pick after the existing ones and removes a picked one', () => {
    const { onChange, box } = setup({ values: ['Done', 'Blocked'] });
    fireEvent.click(box);
    fireEvent.click(screen.getByRole('option', { name: 'Closed' }));
    expect(onChange).toHaveBeenLastCalledWith(['Done', 'Blocked', 'Closed']);
    fireEvent.click(screen.getByRole('option', { name: 'Done' }));
    expect(onChange).toHaveBeenLastCalledWith(['Blocked']);
    expect(screen.getByRole('listbox')).toBeInTheDocument();
  });

  it('moves with the arrows inside the search and toggles with Enter or Space', () => {
    const { onChange, box } = setup();
    fireEvent.click(box);
    press(search(), 'ArrowDown');
    press(search(), 'ArrowDown');
    expect(search()).toHaveAttribute(
      'aria-activedescendant',
      screen.getByRole('option', { name: 'Done' }).id,
    );
    press(search(), 'Enter');
    expect(onChange).toHaveBeenLastCalledWith(['Done']);
    press(search(), 'ArrowUp');
    press(search(), 'ArrowUp');
    press(search(), 'ArrowUp');
    press(search(), ' ');
    expect(onChange).toHaveBeenLastCalledWith(['Blocked']);
    expect(search()).toHaveFocus();
  });

  it('lets a space type once the search has text, and does nothing on Enter without a match', () => {
    const { onChange, box } = setup();
    fireEvent.click(box);
    fireEvent.change(search(), { target: { value: 'in ' } });
    press(search(), ' ');
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.change(search(), { target: { value: 'zzz' } });
    press(search(), 'Enter');
    press(search(), 'ArrowDown');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('closes on Escape without leaking it, and refocuses the trigger', () => {
    const { box, outer } = setup();
    fireEvent.click(box);
    fireEvent.change(search(), { target: { value: 're' } });
    press(search(), 'Escape');
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(box).toHaveFocus();
    expect(outer).not.toHaveBeenCalledWith('Escape');
    fireEvent.click(box);
    expect(search()).toHaveValue('');
  });

  it('closes on Tab and on a click outside, and opens with ArrowDown', () => {
    const { box } = setup();
    fireEvent.click(box);
    press(search(), 'Tab');
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(box).toHaveFocus();
    press(box, 'ArrowDown');
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole('option', { name: 'Done' }));
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    fireEvent.mouseDown(screen.getByRole('button', { name: 'elsewhere' }));
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('follows its anchor when the page scrolls and closes once the anchor is off screen', () => {
    const { box } = setup();
    let top = 100;
    vi.spyOn(box, 'getBoundingClientRect').mockImplementation(
      () => ({ top, bottom: top + 30, left: 10, right: 110, width: 100, height: 30 }) as DOMRect,
    );
    fireEvent.click(box);
    const list = screen.getByRole('listbox').closest('[style]') as HTMLElement;
    expect(list.style.top).toBe('132px');
    fireEvent.scroll(screen.getByRole('listbox'));
    expect(list.style.top).toBe('132px');
    top = 60;
    fireEvent.scroll(window);
    expect(screen.getByRole('listbox')).toBeInTheDocument();
    expect(list.style.top).toBe('92px');
    top = -80;
    fireEvent.scroll(window);
    expect(screen.queryByRole('listbox')).toBeNull();
    fireEvent.click(box);
    fireEvent.click(box);
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('disables the unpicked options at max', () => {
    const { onChange, box } = setup({ values: ['Done'], max: 1 });
    fireEvent.click(box);
    const closed = screen.getByRole('option', { name: 'Closed' });
    expect(closed).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByRole('option', { name: 'Done' })).not.toHaveAttribute('aria-disabled');
    fireEvent.click(closed);
    press(search(), 'Enter');
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('option', { name: 'Done' }));
    expect(onChange).toHaveBeenCalledExactlyOnceWith([]);
  });

  it('lists the picks as chips that remove themselves, including ones not in the options', () => {
    const { onChange } = setup({ values: ['Done', 'Archived'] });
    const chip = screen.getByRole('button', { name: 'Remove Archived' });
    fireEvent.click(chip);
    expect(onChange).toHaveBeenCalledExactlyOnceWith(['Done']);
  });

  it('is inert when disabled', () => {
    const { box } = setup({ values: ['Done'], disabled: true });
    expect(box).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Remove Done' })).toBeDisabled();
  });

  it('works controlled end to end', () => {
    function Host() {
      const [values, setValues] = useState<string[]>([]);
      return <MultiSelect label="S" values={values} options={OPTIONS} onChange={setValues} />;
    }
    render(<Host />);
    fireEvent.click(screen.getByRole('combobox', { name: 'S' }));
    fireEvent.click(screen.getByRole('option', { name: 'To Do' }));
    fireEvent.click(screen.getByRole('option', { name: 'Closed' }));
    expect(screen.getByRole('combobox', { name: 'S' })).toHaveTextContent('To Do +1');
    const chips = within(screen.getByRole('list'));
    fireEvent.click(chips.getByRole('button', { name: 'Remove To Do' }));
    expect(screen.getByRole('combobox', { name: 'S' })).toHaveTextContent('Closed');
  });
});
