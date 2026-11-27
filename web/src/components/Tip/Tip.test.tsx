import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { Tip } from './Tip';

describe('Tip', () => {
  it('carries the text as data-tip around its control, which stays reachable', () => {
    render(
      <Tip tip="Merge after #1">
        <button type="button">Merge</button>
      </Tip>,
    );
    expect(screen.getByRole('button', { name: 'Merge' }).parentElement).toHaveAttribute(
      'data-tip',
      'Merge after #1',
    );
  });

  it('can sit on the left', () => {
    const { container } = render(
      <Tip tip="x" side="left">
        <span>a</span>
      </Tip>,
    );
    expect(container.firstElementChild?.className).toMatch(/left/);
  });
});
