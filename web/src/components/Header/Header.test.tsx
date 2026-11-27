import { QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { useRef, type ReactElement } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Snapshot, SyncStatus } from '@/api/client';
import { snapshot } from '@/test-fixtures';
import { makeQueryClient } from '@/app/providers';
import type { Mode, ThemeSetting } from '@/theme/theme';
import { useScrolled } from '../Shell/useSidebar';
import { ToastProvider } from '../Toast/Toast';

const api = vi.hoisted(() => ({ syncNow: vi.fn() }));
vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  ...api,
}));

import { AUTO_TOAST } from './ThemeButton';
import { Header, type HeaderProps } from './Header';
import { SearchBox } from './SearchBox';

const base = snapshot([]);

function Bar(props: Partial<HeaderProps> & { stuckFromScroll?: boolean }) {
  const burger = useRef<HTMLButtonElement>(null);
  const scrolled = useScrolled();
  return (
    <Header
      stuck={props.stuck ?? scrolled}
      sidebarOpen={false}
      sidebarId="side"
      onToggleSidebar={() => {}}
      burgerRef={burger}
      query=""
      onQuery={() => {}}
      snapshot={base}
      status={null}
      theme="light"
      mode="light"
      onTheme={() => {}}
      {...props}
    />
  );
}

/** Renders with a query client and the toasts. `rerender` takes the new element only. */
function draw(ui: ReactElement) {
  const client = makeQueryClient();
  const wrap = (el: ReactElement) => (
    <QueryClientProvider client={client}>
      <ToastProvider>{el}</ToastProvider>
    </QueryClientProvider>
  );
  const view = render(wrap(ui));
  return { ...view, rerender: (el: ReactElement) => view.rerender(wrap(el)) };
}

function setScrollY(y: number) {
  Object.defineProperty(window, 'scrollY', { configurable: true, value: y });
}

beforeEach(() => {
  api.syncNow.mockReset();
  api.syncNow.mockResolvedValue({ running: true });
  setScrollY(0);
});
afterEach(() => vi.useRealTimers());

describe('Header brand', () => {
  it('shows the name followed by a decorative icon, in the title bar and in the left pill', () => {
    draw(<Bar />);
    const heading = screen.getByRole('heading', { level: 1, name: 'PRDashboards' });
    expect(heading.lastElementChild!.tagName.toLowerCase()).toBe('svg');
    expect(heading.lastElementChild).toHaveAttribute('aria-hidden', 'true');
    const pillName = screen.getAllByText('PRDashboards').find((n) => n.closest('[aria-hidden]'))!;
    const title = pillName.parentElement!;
    expect(title).toHaveAttribute('aria-hidden', 'true');
    expect(title.querySelector('svg')).not.toBeNull();
  });

  it('has the burger and no Sync, Account or Settings button', () => {
    draw(<Bar />);
    expect(screen.getByRole('button', { name: 'Settings sidebar' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sync' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Account' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Settings' })).toBeNull();
  });

  it('toggles the sidebar from the burger', () => {
    const onToggleSidebar = vi.fn();
    draw(<Bar onToggleSidebar={onToggleSidebar} sidebarOpen />);
    const burger = screen.getByRole('button', { name: 'Settings sidebar' });
    expect(burger).toHaveAttribute('aria-expanded', 'true');
    expect(burger).toHaveAttribute('aria-controls', 'side');
    fireEvent.click(burger);
    expect(onToggleSidebar).toHaveBeenCalledTimes(1);
  });
});

describe('useScrolled and the pills', () => {
  const pill = () => screen.getByRole('button', { name: 'Settings sidebar' }).parentElement!;

  it('turn to glass only after scrolling past 24px', () => {
    draw(<Bar />);
    expect(pill()).toHaveAttribute('data-stuck', 'false');
    setScrollY(24);
    fireEvent.scroll(window);
    expect(pill()).toHaveAttribute('data-stuck', 'false');
    setScrollY(25);
    fireEvent.scroll(window);
    expect(pill()).toHaveAttribute('data-stuck', 'true');
    setScrollY(0);
    fireEvent.scroll(window);
    expect(pill()).toHaveAttribute('data-stuck', 'false');
  });

  it('start stuck when the page loads already scrolled', () => {
    setScrollY(300);
    draw(<Bar />);
    expect(pill()).toHaveAttribute('data-stuck', 'true');
  });
});

describe('search', () => {
  it('is a search field in the bar, bound to the query', () => {
    const onQuery = vi.fn();
    draw(<Bar query="abc" onQuery={onQuery} />);
    const field = screen.getByPlaceholderText('Title, repo, PR or Jira key');
    expect(field).toHaveValue('abc');
    expect(field).toHaveAttribute('type', 'search');
    expect(field).toHaveAccessibleName('Search pull requests');
    fireEvent.change(field, { target: { value: 'pay-1' } });
    expect(onQuery).toHaveBeenLastCalledWith('pay-1');
  });

  it('has the full-width field of narrow screens, with its own placeholder', () => {
    const onQuery = vi.fn();
    draw(<SearchBox value="x" onChange={onQuery} variant="page" />);
    const field = screen.getByPlaceholderText('Search pull requests');
    expect(field).toHaveValue('x');
    fireEvent.change(field, { target: { value: 'y' } });
    expect(onQuery).toHaveBeenLastCalledWith('y');
  });
});

describe('refresh button', () => {
  const refresh = () => screen.getByRole('button', { name: 'Refresh' });

  it('starts a sync', async () => {
    draw(<Bar />);
    fireEvent.click(refresh());
    await waitFor(() => expect(api.syncNow).toHaveBeenCalledTimes(1));
  });

  it('is busy, and does not start another sync, while one runs', () => {
    draw(
      <Bar
        status={{
          running: true,
          githubAt: null,
          jiraAt: null,
          nextGithubAt: null,
          nextJiraAt: null,
        }}
      />,
    );
    expect(refresh()).toHaveAttribute('aria-busy', 'true');
    expect(refresh()).toHaveAttribute('data-running', 'true');
    expect(refresh()).toBeEnabled();
    fireEvent.click(refresh());
    expect(api.syncNow).not.toHaveBeenCalled();
  });

  it('shows the source status and sync times in a popover on hover, and closes it on leave', () => {
    draw(<Bar />);
    expect(screen.queryByRole('tooltip')).toBeNull();
    fireEvent.mouseEnter(refresh().parentElement!);
    const tip = screen.getByRole('tooltip');
    expect(refresh()).toHaveAttribute('aria-describedby', tip.id);
    const status = within(tip).getByRole('status', { name: 'Source status' });
    expect(status).toHaveTextContent('GitHub ok');
    expect(status).toHaveTextContent('Jira ok');
    expect(tip).toHaveTextContent(/GitHub synced/);
    expect(tip).toHaveTextContent(/Jira synced/);
    fireEvent.mouseLeave(refresh().parentElement!);
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('opens on keyboard focus and closes on blur and on Escape', () => {
    draw(<Bar />);
    fireEvent.focus(refresh());
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.blur(refresh());
    expect(screen.queryByRole('tooltip')).toBeNull();
    fireEvent.focus(refresh());
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).toBeNull();
  });

  it('shows when a GitHub rate limit resets and that Jira is unreachable', () => {
    const resetAt = new Date(Date.now() + 90_000).toISOString();
    const sources: Snapshot['sources'] = {
      ...base.sources,
      github: {
        state: 'rate_limited',
        syncedAt: base.sources.github.syncedAt,
        error: null,
        resetAt,
      },
      jira: { state: 'unreachable', syncedAt: null, error: 'down', resetAt: null },
    };
    draw(<Bar snapshot={{ ...base, sources }} />);
    fireEvent.focus(refresh());
    const status = screen.getByRole('status', { name: 'Source status' });
    expect(status).toHaveTextContent(/GitHub rate limited, resets in 1m \d\ds/);
    expect(status).toHaveTextContent('Jira unreachable');
  });

  it('shows the next sync countdown', () => {
    const next = new Date(Date.now() + 40_000).toISOString();
    const status: SyncStatus = {
      running: false,
      githubAt: base.sources.github.syncedAt,
      jiraAt: null,
      nextGithubAt: next,
      nextJiraAt: null,
    };
    draw(<Bar status={status} />);
    fireEvent.focus(refresh());
    expect(screen.getByRole('tooltip')).toHaveTextContent(/Next sync in \d+s/);
  });
});

describe('the toast after a refresh', () => {
  const idle: SyncStatus = {
    running: false,
    githubAt: null,
    jiraAt: null,
    nextGithubAt: null,
    nextJiraAt: null,
  };

  function Pair({ snap, status }: { snap: Snapshot; status: SyncStatus }) {
    return <Bar snapshot={snap} status={status} />;
  }

  it('says nothing changed once the sync ended and a newer snapshot arrived', async () => {
    const at = new Date(2026, 10, 24, 10, 26).toISOString();
    const first = snapshot([], { version: 1, generatedAt: at });
    const { rerender } = draw(<Pair snap={first} status={idle} />);
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    const again = (snap: Snapshot, status: SyncStatus) =>
      rerender(<Pair snap={snap} status={status} />);
    await act(async () => again(first, { ...idle, running: true }));
    // The sync ended, but the new snapshot has not arrived: no toast yet.
    await act(async () => again(first, idle));
    expect(screen.queryByText(/^Refreshed/)).toBeNull();
    await act(async () => again({ ...first, version: 2 }, idle));
    expect(screen.getByText('Refreshed. Nothing changed since 10:26.')).toBeInTheDocument();
  });

  it('does not toast a refresh that this button did not start', async () => {
    const first = snapshot([], { version: 1 });
    const { rerender } = draw(<Pair snap={first} status={idle} />);
    await act(async () => rerender(<Pair snap={{ ...first, version: 2 }} status={idle} />));
    expect(screen.queryByText(/^Refreshed/)).toBeNull();
  });
});

describe('theme button', () => {
  function Theme({
    theme,
    mode,
    onTheme,
  }: {
    theme?: ThemeSetting;
    mode?: Mode;
    onTheme: (t: ThemeSetting) => void;
  }) {
    return <Bar theme={theme} mode={mode} onTheme={onTheme} />;
  }
  const button = (name: string) => screen.getByRole('button', { name });

  it('names the state: light, dark or auto', () => {
    const { unmount } = draw(<Theme theme="light" mode="light" onTheme={() => {}} />);
    expect(button('Theme: light')).toBeInTheDocument();
    unmount();
    const dark = draw(<Theme theme="dark" mode="dark" onTheme={() => {}} />);
    expect(button('Theme: dark')).toBeInTheDocument();
    dark.unmount();
    draw(<Theme theme="auto" mode="dark" onTheme={() => {}} />);
    expect(button('Theme: auto')).toBeInTheDocument();
  });

  it('switches between light and dark on a click, also out of Auto', () => {
    const onTheme = vi.fn();
    const { unmount } = draw(<Theme theme="light" mode="light" onTheme={onTheme} />);
    fireEvent.click(button('Theme: light'));
    expect(onTheme).toHaveBeenLastCalledWith('dark');
    unmount();
    const dark = draw(<Theme theme="dark" mode="dark" onTheme={onTheme} />);
    fireEvent.click(button('Theme: dark'));
    expect(onTheme).toHaveBeenLastCalledWith('light');
    dark.unmount();
    draw(<Theme theme="auto" mode="dark" onTheme={onTheme} />);
    fireEvent.click(button('Theme: auto'));
    expect(onTheme).toHaveBeenLastCalledWith('light');
  });

  it('turns Auto on with a right-click and says so', () => {
    const onTheme = vi.fn();
    draw(<Theme theme="dark" mode="dark" onTheme={onTheme} />);
    const notPrevented = fireEvent.contextMenu(button('Theme: dark'));
    expect(notPrevented).toBe(false);
    expect(onTheme).toHaveBeenLastCalledWith('auto');
    expect(screen.getByRole('status')).toHaveTextContent(
      'Theme is Auto: light from 07:00 to 19:00',
    );
    expect(AUTO_TOAST).toBe('Theme is Auto: light from 07:00 to 19:00');
  });

  it('turns Auto on with Shift+click, for the keyboard', () => {
    const onTheme = vi.fn();
    draw(<Theme theme="light" mode="light" onTheme={onTheme} />);
    fireEvent.click(button('Theme: light'), { shiftKey: true });
    expect(onTheme).toHaveBeenCalledTimes(1);
    expect(onTheme).toHaveBeenLastCalledWith('auto');
  });

  it('goes back to the mode chosen before Auto, with a second right-click', () => {
    const onTheme = vi.fn();
    const view = draw(<Theme theme="light" mode="light" onTheme={onTheme} />);
    fireEvent.contextMenu(button('Theme: light'));
    expect(onTheme).toHaveBeenLastCalledWith('auto');
    view.rerender(<Theme theme="auto" mode="dark" onTheme={onTheme} />);
    fireEvent.contextMenu(button('Theme: auto'));
    expect(onTheme).toHaveBeenLastCalledWith('light');
    expect(screen.getByText('Theme is light again')).toBeInTheDocument();
  });

  it('is disabled until the settings load', () => {
    const onTheme = vi.fn();
    draw(<Theme onTheme={onTheme} theme={undefined} mode={undefined} />);
    const b = button('Theme: light');
    expect(b).toBeDisabled();
    fireEvent.contextMenu(b);
    expect(onTheme).not.toHaveBeenCalled();
  });
});

describe('history button', () => {
  it('is absent until the history view is given', () => {
    draw(<Bar />);
    expect(screen.queryByRole('button', { name: 'History' })).toBeNull();
  });

  it('toggles the view and reports it with aria-pressed', () => {
    const onToggle = vi.fn();
    const view = draw(<Bar history={{ active: false, onToggle }} />);
    const button = screen.getByRole('button', { name: 'History' });
    expect(button).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(button);
    expect(onToggle).toHaveBeenCalledTimes(1);
    view.rerender(<Bar history={{ active: true, onToggle }} />);
    expect(screen.getByRole('button', { name: 'History' })).toHaveAttribute('aria-pressed', 'true');
  });
});
