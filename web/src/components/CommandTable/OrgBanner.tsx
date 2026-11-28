import { memo } from 'react';
import Link from 'next/link';
import type { Snapshot } from '@/api/client';
import { orgSsoUrl } from '@/lib/snapshot';
import { RelativeTime } from './RelativeTime';
import styles from './CommandTable.module.css';

export const OrgBanner = memo(function OrgBanner({ snapshot }: { snapshot: Snapshot }) {
  const { github, jira, blockedOrgs } = snapshot.sources;
  return (
    <>
      {blockedOrgs.map((b) => {
        const url = orgSsoUrl(b.org);
        return (
          <div key={b.org || 'unknown'} className={styles.banner} role="alert">
            <span>
              {b.org
                ? `${b.org} has not authorized this token`
                : 'An organization has not authorized this token'}
              . Its pull requests are missing from this list.
            </span>
            {url && (
              <a className={styles.bannerLink} href={url} target="_blank" rel="noopener noreferrer">
                Authorize
              </a>
            )}
          </div>
        );
      })}
      {github.state === 'rate_limited' && (
        <div className={styles.banner} role="status">
          GitHub rate limit reached. Showing data from <RelativeTime iso={github.syncedAt} /> until
          it resets.
        </div>
      )}
      {github.state === 'error' && (
        <div className={styles.banner} role="status">
          {github.error ?? 'GitHub could not be reached'}. Showing data from{' '}
          <RelativeTime iso={github.syncedAt} />.
        </div>
      )}
      {jira.state === 'unreachable' && (
        <div className={styles.banner} role="status">
          {jira.error ?? 'Jira unreachable, retry the lookup'}
        </div>
      )}
      {snapshot.warnings.map((w) => (
        <div key={w.code} className={styles.banner} role="status">
          {w.message}
        </div>
      ))}
      {github.state === 'disconnected' && (
        <div className={styles.notice}>
          GitHub is not connected. <Link href="/accounts/">Connect GitHub</Link>
        </div>
      )}
    </>
  );
});
