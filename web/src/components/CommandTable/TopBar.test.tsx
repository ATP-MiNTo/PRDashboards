import { fireEvent, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import { TopBar } from './TopBar';

const base = snapshot([]);
const onAccount = vi.fn();
const onSettings = vi.fn();

function bar(over: Partial<typeof base.sources>) {
  renderWithClient(
    <TopBar
      snapshot={{ ...base, sources: { ...base.sources, ...over } }}
      status={null}
      onAccount={onAccount}
      onSettings={onSettings}
    />,
  );
}

describe('TopBar sources', () => {
  it('shows both sources as ok', () => {
    bar({});
    expect(screen.getByRole('status', { name: 'Source status' })).toHaveTextContent('GitHub ok');
    expect(screen.getByRole('status', { name: 'Source status' })).toHaveTextContent('Jira ok');
  });

  it('shows when a GitHub rate limit resets and that Jira is unreachable', () => {
    const resetAt = new Date(Date.now() + 90_000).toISOString();
    bar({
      github: {
        state: 'rate_limited',
        syncedAt: base.sources.github.syncedAt,
        error: null,
        resetAt,
      },
      jira: { state: 'unreachable', syncedAt: null, error: 'down', resetAt: null },
    });
    const status = screen.getByRole('status', { name: 'Source status' });
    expect(status).toHaveTextContent(/GitHub rate limited, resets in 1m \d\ds/);
    expect(status).toHaveTextContent('Jira unreachable');
  });
});

describe('TopBar buttons', () => {
  it('has no theme control and no Accounts link', () => {
    bar({});
    expect(screen.queryByRole('group', { name: 'Theme' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Light' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Accounts' })).toBeNull();
  });

  it('opens the account panel and the settings from their buttons', () => {
    bar({});
    const account = screen.getByRole('button', { name: 'Account' });
    expect(account).toHaveAttribute('aria-haspopup', 'dialog');
    fireEvent.click(account);
    expect(onAccount).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Settings' }));
    expect(onSettings).toHaveBeenCalledTimes(1);
  });
});
