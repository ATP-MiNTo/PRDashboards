import { act, fireEvent, render, screen } from '@testing-library/react';
import { useRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Sidebar } from './Sidebar';
import { AUTO_COLLAPSE_MS, SIDEBAR_KEY, useSidebar } from './useSidebar';

function mockWide(wide: boolean) {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: wide,
      media: query,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })),
  );
}

function Harness({ hold = false }: { hold?: boolean }) {
  const sidebar = useSidebar({ hold });
  const burger = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={burger}
        type="button"
        aria-label="Settings sidebar"
        aria-expanded={sidebar.open}
        aria-controls="side"
        onClick={sidebar.toggle}
      />
      <Sidebar
        id="side"
        open={sidebar.open}
        overlay={!sidebar.wide}
        onClose={sidebar.close}
        asideRef={sidebar.aside}
        returnFocus={burger}
      >
        <button type="button">Inside</button>
      </Sidebar>
      <button type="button">Outside</button>
    </>
  );
}

const burger = () => screen.getByRole('button', { name: 'Settings sidebar' });
const collapse = () => screen.getByRole('button', { name: 'Collapse sidebar' });

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('sidebar on a wide screen', () => {
  beforeEach(() => mockWide(true));

  it('is open by default beside the page, as a labelled region with a Settings title', () => {
    render(<Harness />);
    expect(burger()).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('complementary', { name: 'Settings' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Settings' })).toBeInTheDocument();
    expect(screen.queryByTestId('sidebar-scrim')).toBeNull();
    expect(screen.getByRole('button', { name: 'Inside' })).toBeInTheDocument();
  });

  it('closes from the chevron and opens from the burger, and saves the choice', () => {
    render(<Harness />);
    fireEvent.click(collapse());
    expect(burger()).toHaveAttribute('aria-expanded', 'false');
    expect(localStorage.getItem(SIDEBAR_KEY)).toBe('0');
    fireEvent.click(burger());
    expect(burger()).toHaveAttribute('aria-expanded', 'true');
    expect(localStorage.getItem(SIDEBAR_KEY)).toBe('1');
  });

  it('restores the saved state', () => {
    localStorage.setItem(SIDEBAR_KEY, '0');
    render(<Harness />);
    expect(burger()).toHaveAttribute('aria-expanded', 'false');
  });

  it('works when storage throws', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    render(<Harness />);
    expect(burger()).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(burger());
    expect(burger()).toHaveAttribute('aria-expanded', 'false');
    vi.restoreAllMocks();
  });

  describe('auto-collapse', () => {
    beforeEach(() => vi.useFakeTimers());
    const advance = (ms: number) => act(() => vi.advanceTimersByTime(ms));

    it('folds after 30 seconds without saving the choice', () => {
      render(<Harness />);
      advance(AUTO_COLLAPSE_MS - 1);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      advance(1);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
      expect(localStorage.getItem(SIDEBAR_KEY)).toBeNull();
    });

    it('waits while the pointer is inside and counts again once it leaves', () => {
      render(<Harness />);
      fireEvent.mouseMove(screen.getByRole('button', { name: 'Inside' }));
      advance(120_000);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      fireEvent.mouseMove(screen.getByRole('button', { name: 'Outside' }));
      advance(AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
    });

    it('waits while the focus is inside the sidebar, and counts again once it leaves', () => {
      render(<Harness />);
      const inside = screen.getByRole('button', { name: 'Inside' });
      inside.focus();
      advance(120_000);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      // Focus moves out with the keyboard: it counts again.
      screen.getByRole('button', { name: 'Outside' }).focus();
      advance(AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
    });

    it('does not fold while a button the pointer clicked in the sidebar still has the focus', () => {
      render(<Harness />);
      const inside = screen.getByRole('button', { name: 'Inside' });
      fireEvent.mouseMove(inside);
      fireEvent.pointerDown(inside);
      inside.focus();
      // The pointer leaves; the clicked button keeps the focus, and that keeps the sidebar open.
      fireEvent.mouseMove(screen.getByRole('button', { name: 'Outside' }));
      expect(inside).toHaveFocus();
      advance(10 * AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      // Once the focus leaves, it folds after the delay.
      inside.blur();
      advance(AUTO_COLLAPSE_MS - 1);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      advance(1);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
    });

    it('does not fold on pointer movement while a key press left the focus in the sidebar', () => {
      render(<Harness />);
      const inside = screen.getByRole('button', { name: 'Inside' });
      fireEvent.keyDown(inside, { key: 'Escape' });
      inside.focus();
      fireEvent.mouseMove(screen.getByRole('button', { name: 'Outside' }));
      advance(10 * AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
    });

    it('stays open while a dialog is open, and folds 30 seconds after it closed and the focus left', () => {
      render(<Harness />);
      const inside = screen.getByRole('button', { name: 'Inside' });
      fireEvent.pointerDown(inside);
      inside.focus();
      fireEvent.mouseMove(screen.getByRole('button', { name: 'Outside' }));
      const dialog = document.createElement('div');
      dialog.setAttribute('role', 'dialog');
      dialog.setAttribute('aria-modal', 'true');
      document.body.append(dialog);
      advance(5 * AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      dialog.remove();
      inside.blur();
      advance(AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
    });

    it('waits while any dialog is open over the page', () => {
      render(<Harness />);
      const dialog = document.createElement('div');
      dialog.setAttribute('role', 'dialog');
      dialog.setAttribute('aria-modal', 'true');
      document.body.append(dialog);
      advance(120_000);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      dialog.remove();
      advance(AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
    });

    it('waits while a panel is open', () => {
      const { rerender } = render(<Harness hold />);
      advance(120_000);
      expect(burger()).toHaveAttribute('aria-expanded', 'true');
      rerender(<Harness hold={false} />);
      advance(AUTO_COLLAPSE_MS);
      expect(burger()).toHaveAttribute('aria-expanded', 'false');
    });
  });
});

describe('sidebar on a narrow screen', () => {
  beforeEach(() => mockWide(false));

  it('starts closed and does not read the saved state', () => {
    localStorage.setItem(SIDEBAR_KEY, '1');
    render(<Harness />);
    expect(burger()).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Inside' })).toBeNull();
  });

  it('opens as a modal dialog from the burger without saving', () => {
    render(<Harness />);
    fireEvent.click(burger());
    const dialog = screen.getByRole('dialog', { name: 'Settings' });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(dialog).toHaveFocus();
    expect(localStorage.getItem(SIDEBAR_KEY)).toBeNull();
  });

  it('closes on Escape, on the scrim and on the chevron, and focuses the burger', () => {
    render(<Harness />);
    fireEvent.click(burger());
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(burger()).toHaveFocus();

    fireEvent.click(burger());
    fireEvent.click(screen.getByTestId('sidebar-scrim'));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(burger()).toHaveFocus();

    fireEvent.click(burger());
    fireEvent.click(collapse());
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(burger()).toHaveFocus();
  });

  it('keeps Tab inside the dialog', () => {
    render(<Harness />);
    fireEvent.click(burger());
    const dialog = screen.getByRole('dialog');
    const last = screen.getByRole('button', { name: 'Inside' });
    last.focus();
    fireEvent.keyDown(last, { key: 'Tab' });
    expect(collapse()).toHaveFocus();
    fireEvent.keyDown(collapse(), { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
    dialog.focus();
    fireEvent.keyDown(dialog, { key: 'Tab', shiftKey: true });
    expect(last).toHaveFocus();
  });
});
