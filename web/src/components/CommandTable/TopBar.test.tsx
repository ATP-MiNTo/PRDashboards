import { screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { snapshot } from '@/test-fixtures';
import { renderWithClient } from '@/test-utils';
import { TopBar } from './TopBar';

vi.mock('next/link', () => ({
  default: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

const base = snapshot([]);

function bar(over: Partial<typeof base.sources>) {
  renderWithClient(
    <TopBar
      snapshot={{ ...base, sources: { ...base.sources, ...over } }}
      status={null}
      theme="auto"
      onTheme={vi.fn()}
      onSettings={vi.fn()}
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
