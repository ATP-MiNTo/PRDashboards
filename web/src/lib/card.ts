/** Two letters for the avatar: the first two characters of the login, upper case. */
export function initials(author: string | null): string {
  if (!author) return '?';
  return Array.from(author).slice(0, 2).join('').toUpperCase();
}

/** How long ago, in the card's short form: 8m, 3h, 2d. Empty when the time is unknown. */
export function ageShort(iso: string | null, now: number): string {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return '';
  const min = Math.max(0, Math.round((now - t) / 60_000));
  if (min < 60) return `${min}m`;
  if (min < 1440) return `${Math.round(min / 60)}h`;
  return `${Math.round(min / 1440)}d`;
}
