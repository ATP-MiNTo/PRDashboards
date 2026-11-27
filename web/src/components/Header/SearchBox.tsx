'use client';

import styles from './Header.module.css';

/**
 * The search field. In the header (`bar`) it shows from 768px up with the longer placeholder; above
 * the stats (`page`) it is the full-width field of narrow screens. The design has no magnifier:
 * the icon it draws collapses to no width.
 */
export function SearchBox({
  value,
  onChange,
  variant,
}: {
  value: string;
  onChange: (q: string) => void;
  variant: 'bar' | 'page';
}) {
  const bar = variant === 'bar';
  return (
    <label className={bar ? styles.searchBar : styles.searchPage}>
      <input
        type="search"
        aria-label="Search pull requests"
        placeholder={bar ? 'Title, repo, PR or Jira key' : 'Search pull requests'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}
