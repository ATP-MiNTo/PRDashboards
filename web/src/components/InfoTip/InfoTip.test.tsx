import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { InfoTip } from './InfoTip';

describe('InfoTip', () => {
  it('is a button whose accessible description is the note', () => {
    render(<InfoTip tip="Rules run top to bottom." />);
    const button = screen.getByRole('button', { name: 'About this section' });
    expect(button).toHaveAccessibleDescription('Rules run top to bottom.');
  });

  it('shows the note under the button on hover and focus, and hides it again', () => {
    render(<InfoTip tip="The note." label="About rules" />);
    const button = screen.getByRole('button', { name: 'About rules' });
    expect(document.body.querySelectorAll('[aria-hidden="true"]:not(span)')).toHaveLength(0);
    fireEvent.mouseEnter(button);
    expect(screen.getAllByText('The note.')).toHaveLength(2);
    fireEvent.mouseLeave(button);
    expect(screen.getAllByText('The note.')).toHaveLength(1);
    fireEvent.focus(button);
    expect(screen.getAllByText('The note.')).toHaveLength(2);
    fireEvent.blur(button);
    expect(screen.getAllByText('The note.')).toHaveLength(1);
  });

  it('closes the note with Escape without closing what is around it', () => {
    let outer = 0;
    render(
      <div onKeyDown={() => outer++}>
        <InfoTip tip="The note." />
      </div>,
    );
    const button = screen.getByRole('button', { name: 'About this section' });
    fireEvent.focus(button);
    fireEvent.keyDown(button, { key: 'Escape' });
    expect(screen.getAllByText('The note.')).toHaveLength(1);
    expect(outer).toBe(0);
    fireEvent.keyDown(button, { key: 'Escape' });
    expect(outer).toBe(1);
  });
});
