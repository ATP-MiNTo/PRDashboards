import { render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import Done from './page';

function at(search: string) {
  window.history.replaceState(null, '', `/auth/done/${search}`);
}

afterEach(() => at(''));

describe('Auth done page', () => {
  it('confirms a connection and links back', async () => {
    at('?provider=github&result=ok');
    render(<Done />);
    expect(await screen.findByText('GitHub is connected.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Back to the dashboard' })).toHaveAttribute(
      'href',
      '/',
    );
  });

  it('names Jira for the atlassian provider', async () => {
    at('?provider=atlassian&result=ok');
    render(<Done />);
    expect(await screen.findByText('Jira is connected.')).toBeInTheDocument();
  });

  it.each([
    ['denied', /access was not granted/],
    ['invalid_state', /expired or was already used/],
    ['signed_out', /session ended/],
    ['error', /Could not connect GitHub/],
  ])('explains %s', async (result, text) => {
    at(`?provider=github&result=${result}`);
    render(<Done />);
    expect(await screen.findByText(text)).toBeInTheDocument();
  });

  it('links to setup when the app is not configured', async () => {
    at('?provider=atlassian&result=not_configured');
    render(<Done />);
    expect(await screen.findByRole('link', { name: 'Go to setup' })).toHaveAttribute(
      'href',
      expect.stringMatching(/^\/setup\/?$/),
    );
  });

  it('treats unknown values as an error and never echoes them', async () => {
    at('?provider=%3Cb%3Ex&result=%3Cscript%3E');
    render(<Done />);
    expect(
      await screen.findByText('Could not connect The provider. Try again.'),
    ).toBeInTheDocument();
    expect(screen.queryByText(/script/)).toBeNull();
  });
});
