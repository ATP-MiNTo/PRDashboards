import { pageItems, pageRange, PAGE_SIZES } from '@/lib/view';
import type { Group } from '@/lib/snapshot';
import styles from './Toolbar.module.css';

export interface PagerProps {
  pages: Group[][];
  page: number;
  pageSize: number;
  onPage: (page: number) => void;
  onPageSize: (size: number) => void;
}

function Chevron({ d }: { d: string }) {
  return (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d={d} />
    </svg>
  );
}

/** "Showing X to Y of N" on the left; page size and page buttons on the right. */
export function Pager({ pages, page, pageSize, onPage, onPageSize }: PagerProps) {
  if (pages.length === 0) return null;
  const range = pageRange(pages, page);
  return (
    <div className={styles.pager}>
      <span className={styles.showing}>
        Showing {range.from} to {range.to} of {range.total}
      </span>
      <div className={styles.pagerRight}>
        <select
          className={styles.select}
          aria-label="Pull requests per page"
          value={pageSize}
          onChange={(e) => onPageSize(Number(e.target.value))}
        >
          {PAGE_SIZES.map((n) => (
            <option key={n} value={n}>
              {n} per page
            </option>
          ))}
        </select>
        {pages.length > 1 && (
          <nav className={styles.pages} aria-label="Pages">
            <button
              type="button"
              className={styles.pageButton}
              aria-label="Previous page"
              disabled={page === 0}
              onClick={() => onPage(page - 1)}
            >
              <Chevron d="m15 6-6 6 6 6" />
            </button>
            {pageItems(page, pages.length).map((p, i) =>
              p === null ? (
                <span key={`gap${i}`} className={styles.gap} aria-hidden="true">
                  …
                </span>
              ) : (
                <button
                  key={p}
                  type="button"
                  className={
                    p === page ? `${styles.pageButton} ${styles.current}` : styles.pageButton
                  }
                  aria-label={`Page ${p + 1}`}
                  aria-current={p === page ? 'page' : undefined}
                  onClick={() => onPage(p)}
                >
                  {p + 1}
                </button>
              ),
            )}
            <button
              type="button"
              className={styles.pageButton}
              aria-label="Next page"
              disabled={page === pages.length - 1}
              onClick={() => onPage(page + 1)}
            >
              <Chevron d="m9 6 6 6-6 6" />
            </button>
          </nav>
        )}
      </div>
    </div>
  );
}
