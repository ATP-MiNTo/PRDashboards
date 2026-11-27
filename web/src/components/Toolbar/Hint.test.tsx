import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Hint } from './Hint';

describe('Hint', () => {
  it('opens when the label gets focus and links it with aria-describedby', () => {
    render(<Hint label="Hidden PRs">Three states.</Hint>);
    const label = screen.getByText('Hidden PRs');
    expect(screen.queryByRole('tooltip')).toBeNull();
    fireEvent.focus(label);
    const tip = screen.getByRole('tooltip');
    expect(tip).toHaveTextContent('Three states.');
    expect(label).toHaveAttribute('aria-describedby', tip.id);
  });

  it('closes on blur and on Escape', () => {
    render(<Hint label="Hidden PRs">Three states.</Hint>);
    const label = screen.getByText('Hidden PRs');
    fireEvent.focus(label);
    fireEvent.blur(label);
    expect(screen.queryByRole('tooltip')).toBeNull();
    fireEvent.focus(label);
    fireEvent.keyDown(label, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).toBeNull();
    expect(label).not.toHaveAttribute('aria-describedby');
  });

  it('opens on hover and closes when the pointer leaves or Escape is pressed', () => {
    render(<Hint label="Hidden PRs">Three states.</Hint>);
    const wrap = screen.getByText('Hidden PRs').parentElement!;
    fireEvent.mouseEnter(wrap);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.mouseLeave(wrap);
    expect(screen.queryByRole('tooltip')).toBeNull();
    fireEvent.mouseEnter(wrap);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('can be reached with the keyboard', () => {
    render(<Hint label="Hidden PRs">Three states.</Hint>);
    expect(screen.getByText('Hidden PRs')).toHaveAttribute('tabindex', '0');
  });
});
