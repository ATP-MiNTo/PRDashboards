'use client';

import { useEffect, useRef } from 'react';
import { useRevokeAll, useSaveSettings, useSettings } from '@/api/queries';
import type { Settings } from '@/api/client';
import { useTheme } from '@/theme/useTheme';
import type { ThemeSetting } from '@/theme/theme';
import { AccountsPanel } from '../AccountsPanel';
import { ActivitySection } from './ActivitySection';
import { MultiPrSection } from './MultiPrSection';
import { RepositoriesSection } from './RepositoriesSection';
import { RulesSection } from './RulesSection';
import styles from './Settings.module.css';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

const INTERVALS: { value: string; label: string }[] = [
  { value: '60', label: '1 minute' },
  { value: '120', label: '2 minutes' },
  { value: '300', label: '5 minutes' },
  { value: '900', label: '15 minutes' },
  { value: '3600', label: '1 hour' },
];

const THEMES: { value: ThemeSetting; label: string }[] = [
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
  { value: 'auto', label: 'Auto' },
];

function Preferences() {
  const settings = useSettings();
  const save = useSaveSettings();
  const { theme, setTheme } = useTheme();
  const current = settings.data;
  if (!current) return <p className={styles.muted}>Loading settings</p>;
  const update = (patch: Partial<Settings>) => save.mutate({ ...current, ...patch });
  const interval = (field: 'githubSec' | 'jiraSec', label: string) => (
    <label className={styles.field}>
      {label}
      <select
        value={String(current.polling[field])}
        onChange={(e) =>
          update({
            polling: {
              ...current.polling,
              [field]: Number(e.target.value) as Settings['polling']['githubSec'],
            },
          })
        }
      >
        {INTERVALS.map((i) => (
          <option key={i.value} value={i.value}>
            {i.label}
          </option>
        ))}
      </select>
    </label>
  );
  return (
    <>
      <section className={styles.section} aria-labelledby="set-delete">
        <h3 id="set-delete">Delete branch after merge</h3>
        <label className={styles.check}>
          <input
            type="checkbox"
            checked={current.deleteBranchAfterMerge}
            onChange={(e) => update({ deleteBranchAfterMerge: e.target.checked })}
          />
          Delete the head branch after a merge by default
        </label>
      </section>
      <section className={styles.section} aria-labelledby="set-polling">
        <h3 id="set-polling">Polling intervals</h3>
        {interval('githubSec', 'GitHub')}
        {interval('jiraSec', 'Jira')}
      </section>
      <section className={styles.section} aria-labelledby="set-theme">
        <h3 id="set-theme">Theme</h3>
        <div className={styles.segment} role="group" aria-label="Theme">
          {THEMES.map((t) => (
            <button
              key={t.value}
              type="button"
              aria-pressed={theme === t.value}
              onClick={() => setTheme(t.value)}
            >
              {t.label}
            </button>
          ))}
        </div>
      </section>
      {save.isError && (
        <p className={styles.error} role="alert">
          Could not save the settings.
        </p>
      )}
    </>
  );
}

function SessionsSection() {
  const revoke = useRevokeAll();
  return (
    <section className={styles.section} aria-labelledby="set-sessions">
      <h3 id="set-sessions">Sessions</h3>
      <p className={styles.sessionNote}>Signs out every browser, including this one.</p>
      <div className={styles.actions}>
        <button type="button" disabled={revoke.isPending} onClick={() => revoke.mutate(undefined)}>
          Sign out all browsers
        </button>
      </div>
      {revoke.isSuccess && (
        <p className={styles.sessionNote} role="status">
          Signed out everywhere. Open the launch link from scripts/start.sh to sign in again.
        </p>
      )}
      {revoke.isError && (
        <p className={styles.error} role="alert">
          Could not sign out the other browsers.
        </p>
      )}
    </section>
  );
}

export function SettingsDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const dialog = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!open) return;
    const before = document.activeElement as HTMLElement | null;
    dialog.current?.focus();
    return () => before?.focus();
  }, [open]);

  if (!open) return null;

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== 'Tab' || !dialog.current) return;
    const items = [...dialog.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    if (!first || !last) return;
    const at = document.activeElement;
    if (e.shiftKey && (at === first || at === dialog.current)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return (
    <div className={styles.layer} onKeyDown={onKeyDown}>
      <div className={styles.scrim} data-testid="settings-scrim" onClick={onClose} />
      <aside
        ref={dialog}
        className={styles.drawer}
        role="dialog"
        aria-modal="true"
        aria-label="Settings"
        tabIndex={-1}
      >
        <header className={styles.head}>
          <h2>Settings</h2>
          <button
            type="button"
            className={styles.close}
            aria-label="Close settings"
            onClick={onClose}
          >
            Close
          </button>
        </header>
        <div className={styles.body}>
          <AccountsPanel />
          <RepositoriesSection />
          <Preferences />
          <RulesSection />
          <MultiPrSection />
          <SessionsSection />
          <ActivitySection />
        </div>
      </aside>
    </div>
  );
}
