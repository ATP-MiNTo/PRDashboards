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

/** A caret on a menu button. */
export function ChevronDownIcon() {
  return (
    <Icon size={16}>
      <path d="m6 9 6 6 6-6" />
    </Icon>
  );
}

/** The check of the Approve item. */
export function CheckIcon() {
  return (
    <Icon size={16}>
      <path d="m5 12 5 5 9-10" />
    </Icon>
  );
}

/** The mark of the Request changes item. */
export function AlertIcon() {
  return (
    <Icon size={16}>
      <path d="M12 4v10M12 19v.01" />
    </Icon>
  );
}

/** The plus of the add-ticket button. */
export function PlusIcon() {
  return (
    <Icon size={16} strokeWidth={3.2}>
      <path d="M12 5v14M5 12h14" />
    </Icon>
  );
}

/** The rocket of the merge launch. */
export function RocketIcon() {
  return (
    <Icon size={16}>
      <path d="M4.5 16.5c-1.5 1.26-2 5-2 5s3.74-.5 5-2c.71-.84.7-2.13-.09-2.91a2.18 2.18 0 0 0-2.91-.09z" />
      <path d="m12 15-3-3a22 22 0 0 1 2-3.95A12.88 12.88 0 0 1 22 2c0 2.72-.78 7.5-6 11a22.35 22.35 0 0 1-4 2z" />
      <path d="M9 12H4s.55-3.03 2-4c1.62-1.08 5 0 5 0" />
      <path d="M12 15v5s3.03-.55 4-2c1.08-1.62 0-5 0-5" />
    </Icon>
  );
}

/** A spinner for a running check: a ring with a gap that turns. */
export function SpinnerIcon({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="3"
      strokeLinecap="round"
      aria-hidden="true"
      focusable="false"
    >
      <circle cx="12" cy="12" r="9" opacity="0.25" />
      <path d="M12 3a9 9 0 0 1 9 9" />
    </svg>
  );
}
