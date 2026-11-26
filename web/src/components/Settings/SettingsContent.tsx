'use client';

import { useRevokeAll, useSaveSettings, useSettings } from '@/api/queries';
import type { Settings } from '@/api/client';
import { useTheme } from '@/theme/useTheme';
import { DARK_FROM_HOUR, LIGHT_FROM_HOUR } from '@/theme/theme';
import { SidePanel } from '../SidePanel/SidePanel';
import { ActivitySection } from './ActivitySection';
import { MultiPrSection } from './MultiPrSection';
import { RepositoriesSection } from './RepositoriesSection';
import { RulesSection } from './RulesSection';
import styles from './Settings.module.css';
import { ThemeSwap } from './ThemeSwap';

const INTERVALS: { value: string; label: string }[] = [
  { value: '60', label: '1 minute' },
  { value: '120', label: '2 minutes' },
  { value: '300', label: '5 minutes' },
  { value: '900', label: '15 minutes' },
  { value: '3600', label: '1 hour' },
];

function Preferences() {
  const settings = useSettings();
  const save = useSaveSettings();
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
        <div className={styles.pollingLine}>
          {interval('githubSec', 'GitHub')}
          <span aria-hidden="true" className={styles.sep}>
            |
          </span>
          {interval('jiraSec', 'Jira')}
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

const hh = (h: number) => String(h).padStart(2, '0');

function ThemeSection() {
  const { theme, mode, setTheme } = useTheme();
  const loading = theme === undefined;
  return (
    <section className={styles.section} aria-labelledby="set-theme">
      <h3 id="set-theme">Theme</h3>
      <ThemeSwap
        dark={mode === 'dark'}
        disabled={loading}
        onChange={(dark) => setTheme(dark ? 'dark' : 'light')}
      />
      <label className={styles.check}>
        <input
          type="checkbox"
          checked={theme === 'auto'}
          disabled={loading}
          onChange={(e) => setTheme(e.target.checked ? 'auto' : (mode ?? 'light'))}
        />
        Auto (light from {hh(LIGHT_FROM_HOUR)}:00 to {hh(DARK_FROM_HOUR)}:00)
      </label>
    </section>
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
  return (
    <SidePanel open={open} onClose={onClose} title="Settings" closeLabel="Close settings">
      <div className={styles.body}>
        <RepositoriesSection />
        <Preferences />
        <RulesSection />
        <MultiPrSection />
        <SessionsSection />
        <ThemeSection />
        <ActivitySection />
      </div>
    </SidePanel>
  );
}
