import type { ReactNode } from 'react';

function Icon({
  size,
  strokeWidth = 2,
  children,
}: {
  size: number;
  strokeWidth?: number;
  children: ReactNode;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      {children}
    </svg>
  );
}

/** A pull request: when it was last updated. */
export function PrIcon() {
  return (
    <Icon size={13}>
      <circle cx="6" cy="6" r="2.5" />
      <circle cx="6" cy="18" r="2.5" />
      <circle cx="18" cy="18" r="2.5" />
      <path d="M6 8.5v7M18 15.5V10a3 3 0 0 0-3-3h-3" />
      <path d="m14 4-2 3 2 3" />
    </Icon>
  );
}

/** A commit: when the latest one was made. */
export function CommitIcon() {
  return (
    <Icon size={16}>
      <circle cx="12" cy="12" r="3" />
      <path d="M3 12h6M15 12h6" />
    </Icon>
  );
}

/** Layers: a stack of pull requests. */
export function LayersIcon() {
  return (
    <Icon size={14} strokeWidth={2.2}>
      <path d="m12 3 9 5-9 5-9-5z" />
      <path d="m3 13 9 5 9-5" />
    </Icon>
  );
}
