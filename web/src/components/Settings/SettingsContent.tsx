'use client';

import { useEffect, useRef, useState } from 'react';
import { useSaveSettings, useSettings } from '@/api/queries';
import type { Settings } from '@/api/client';
import { POLLING_STEPS, pollingLabel, stepPolling } from '@/lib/polling';
import { useTheme } from '@/theme/useTheme';
import { DARK_FROM_HOUR, LIGHT_FROM_HOUR } from '@/theme/theme';
import { RepositoriesSection } from './RepositoriesSection';
import styles from './Settings.module.css';

/** How long after the last step the new intervals are saved, so a flick of the wheel is one request. */
export const POLL_SAVE_MS = 500;
/** A wheel turns the field at most this often, so a trackpad does not run through every value at once. */
const WHEEL_STEP_MS = 120;

type Polling = Settings['polling'];
type PollField = keyof Polling;

/** A number field that is stepped by the mouse wheel and the arrow keys, never typed into. */
function PollInput({
  label,
  name,
  value,
  onStep,
}: {
  label: string;
  name: string;
  value: number;
  onStep: (dir: 1 | -1) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const step = useRef(onStep);
  useEffect(() => {
    step.current = onStep;
  });
  useEffect(() => {
    const el = input.current;
    if (!el) return;
    let last = 0;
    // A native listener: React's wheel handler is passive and cannot stop the page from scrolling.
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const now = e.timeStamp;
      if (now - last < WHEEL_STEP_MS) return;
      last = now;
      step.current(e.deltaY < 0 ? 1 : -1);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, []);
  return (
    <div className={styles.pollRow}>
      <span>{label}</span>
      <label className={styles.pollBox} title="Scroll or press the arrow keys to change">
        <input
          ref={input}
          type="text"
          inputMode="numeric"
          readOnly
          role="spinbutton"
          aria-label={`${name} polling seconds`}
          aria-valuenow={value}
          aria-valuemin={POLLING_STEPS[0]}
          aria-valuemax={POLLING_STEPS[POLLING_STEPS.length - 1]}
          aria-valuetext={pollingLabel(value)}
          value={value}
          onKeyDown={(e) => {
            if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return;
            e.preventDefault();
            onStep(e.key === 'ArrowUp' ? 1 : -1);
          }}
        />
        <span className={styles.pollUnit}>s</span>
      </label>
    </div>
  );
}

/** GitHub and Jira polling, and the button that opens the status sync rules. */
function PollingSection({ onOpenRules }: { onOpenRules: () => void }) {
  const settings = useSettings();
  const { mutate, isError } = useSaveSettings();
  const current = settings.data;
  const [draft, setDraft] = useState<Partial<Polling>>({});

  // A step shows at once and is saved once the stepping has paused.
  useEffect(() => {
    if (!current || Object.keys(draft).length === 0) return;
    const timer = setTimeout(() => {
      mutate(
        { ...current, polling: { ...current.polling, ...draft } },
        {
          // Keep a value that was stepped again while this request ran.
          onSettled: () =>
            setDraft((d) => {
              const rest = { ...d };
              for (const k of Object.keys(draft) as PollField[]) {
                if (rest[k] === draft[k]) delete rest[k];
              }
              return rest;
            }),
        },
      );
    }, POLL_SAVE_MS);
    return () => clearTimeout(timer);
  }, [current, draft, mutate]);

  if (!current) return <p className={styles.muted}>Loading settings</p>;
  const stepField = (field: PollField) => (dir: 1 | -1) =>
    setDraft((d) => {
      const from = d[field] ?? current.polling[field];
      const to = stepPolling(from, dir);
      return to === from ? d : { ...d, [field]: to };
    });
  return (
    <section className={styles.prefs} aria-label="Polling">
      <PollInput
        label="GitHub polling"
        name="GitHub"
        value={draft.githubSec ?? current.polling.githubSec}
        onStep={stepField('githubSec')}
      />
      <PollInput
        label="Jira polling"
        name="Jira"
        value={draft.jiraSec ?? current.polling.jiraSec}
        onStep={stepField('jiraSec')}
      />
      <button type="button" aria-haspopup="dialog" onClick={onOpenRules}>
        Status sync rules
      </button>
      {isError && (
        <p className={styles.error} role="alert">
          Could not save the settings.
        </p>
      )}
    </section>
  );
}

const hh = (h: number) => String(h).padStart(2, '0');

/** Auto theme. The light and dark choice is the sun and moon button in the header. */
function ThemeSection() {
  const { theme, mode, setTheme } = useTheme();
  return (
    <section className={styles.section} aria-labelledby="set-theme">
      <h3 id="set-theme">Theme</h3>
      <label className={styles.check}>
        <input
          type="checkbox"
          checked={theme === 'auto'}
          disabled={theme === undefined}
          onChange={(e) => setTheme(e.target.checked ? 'auto' : (mode ?? 'light'))}
        />
        Auto (light from {hh(LIGHT_FROM_HOUR)}:00 to {hh(DARK_FROM_HOUR)}:00)
      </label>
    </section>
  );
}

/**
 * The Settings sections, shown in the left sidebar: the repositories, the polling with the button
 * for the status sync rules, and the Auto theme switch, which is the way to it without a pointer.
 * The rules, the repository settings and the accounts open in their own dialogs.
 */
export function SettingsContent({
  onOpenRules,
  off,
  onOff,
}: {
  onOpenRules: () => void;
  /** Ids of the repositories whose pull requests the list leaves out. */
  off: ReadonlySet<string>;
  onOff: (next: Set<string>) => void;
}) {
  return (
    <div className={styles.body}>
      <RepositoriesSection off={off} onOff={onOff} />
      <PollingSection onOpenRules={onOpenRules} />
      <ThemeSection />
    </div>
  );
}
