import { absoluteTime, relativeTime } from '@/lib/snapshot';
import { useNow } from './useNow';

/** A relative time that updates itself, with the exact time as its tooltip. */
export function RelativeTime({ iso }: { iso: string | null }) {
  const now = useNow(30_000);
  return (
    <time dateTime={iso ?? undefined} title={absoluteTime(iso)}>
      {relativeTime(iso, now)}
    </time>
  );
}
