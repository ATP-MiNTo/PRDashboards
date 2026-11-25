import type { Check, SnapshotPr } from '@/api/client';
import { checkDuration, githubChecksUrl, safeLink } from '@/lib/snapshot';
import styles from './CommandTable.module.css';
import { Pill, type Tone } from './Pill';
import { useNow } from './useNow';

const ROLLUP: Record<SnapshotPr['ci']['state'], { tone: Tone; label: string }> = {
  passing: { tone: 'good', label: 'Passing' },
  failing: { tone: 'bad', label: 'Failing' },
  running: { tone: 'info', label: 'Running' },
  none: { tone: 'mute', label: 'No CI' },
};

const STATE_LABEL: Record<Check['state'], string> = {
  passing: 'Passing',
  failing: 'Failing',
  running: 'Running',
  pending: 'Pending',
  skipped: 'Skipped',
  neutral: 'Neutral',
  cancelled: 'Cancelled',
};

function CheckRow({ check }: { check: Check }) {
  const now = useNow(1000);
  const link = safeLink(check.url);
  const duration = checkDuration(check, now);
  const label = `${check.name}: ${STATE_LABEL[check.state]}`;
  return (
    <li className={styles.check}>
      <span
        className={`${styles.dot} ${styles[`dot_${check.state}`]}`}
        role="img"
        aria-label={STATE_LABEL[check.state]}
      />
      {link ? (
        <a
          className={styles.checkName}
          href={link.href}
          target="_blank"
          rel="noopener noreferrer"
          title={label}
        >
          {check.name}
        </a>
      ) : (
        <span className={styles.checkName} title={label}>
          {check.name}
        </span>
      )}
      {link?.host ? <span className={styles.checkApp}>{link.host}</span> : null}
      {!link?.host && check.app ? <span className={styles.checkApp}>{check.app}</span> : null}
      {duration ? <span className={styles.checkTime}>{duration}</span> : null}
    </li>
  );
}

export function CiCell({ pr }: { pr: SnapshotPr }) {
  const rollup = ROLLUP[pr.ci.state];
  const more = pr.ci.total - pr.ci.checks.length;
  // A fine-grained token cannot read check runs: the rollup and the count come back, the list is empty.
  const moreText = pr.ci.checks.length === 0 ? `${more} checks` : `+${more} more`;
  const moreUrl = more > 0 ? githubChecksUrl(pr.repo.nameWithOwner, pr.number) : null;
  return (
    <div className={styles.ci}>
      <Pill tone={rollup.tone}>{rollup.label}</Pill>
      {pr.ci.checks.length > 0 && (
        <ul className={styles.checks} aria-label="Checks">
          {pr.ci.checks.map((c) => (
            <CheckRow key={`${c.name}|${c.app ?? ''}|${c.url ?? ''}`} check={c} />
          ))}
        </ul>
      )}
      {more > 0 &&
        (moreUrl ? (
          <a className={styles.more} href={moreUrl} target="_blank" rel="noopener noreferrer">
            {moreText}
          </a>
        ) : (
          <span className={styles.more}>{moreText}</span>
        ))}
    </div>
  );
}
