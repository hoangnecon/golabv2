import type { Tier } from '@/api/types';

export const NONE = '—';

const TIER_LABEL: Record<Tier, string> = {
  free: 'Free',
  pro: 'Pro',
  pro_plus: 'Pro+',
};

export function formatTier(tier: Tier | undefined): string {
  if (!tier) return NONE;
  return TIER_LABEL[tier] ?? tier;
}

export function formatComputeUnits(balance: number | undefined): string {
  if (balance === undefined || Number.isNaN(balance)) return NONE;
  return `${balance.toFixed(2)} CU`;
}

export function formatBurnRate(hourly: number | undefined): string {
  if (hourly === undefined || Number.isNaN(hourly)) return NONE;
  return `${hourly.toFixed(2)} CU/h`;
}

export function formatGbPair(usedGb: number | undefined, totalGb: number | undefined): string {
  if (usedGb === undefined || totalGb === undefined || totalGb <= 0) return NONE;
  const pct = Math.min(100, Math.max(0, (usedGb / totalGb) * 100));
  return `${usedGb.toFixed(2)} / ${totalGb.toFixed(2)} GB (${pct.toFixed(0)}%)`;
}

export function formatMbAsGbPair(usedMb: number | undefined, totalMb: number | undefined): string {
  if (usedMb === undefined || totalMb === undefined || totalMb <= 0) return NONE;
  const usedGb = usedMb / 1024;
  const totalGb = totalMb / 1024;
  const pct = Math.min(100, Math.max(0, (usedMb / totalMb) * 100));
  return `${usedGb.toFixed(1)} / ${totalGb.toFixed(1)} GB (${pct.toFixed(0)}%)`;
}

export function formatUptime(seconds: number | undefined): string {
  if (seconds === undefined || seconds <= 0) return NONE;
  const hrs = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const secs = Math.floor(seconds % 60);
  if (hrs > 0) return `${hrs}h ${mins}m`;
  if (mins > 0) return `${mins}m ${secs}s`;
  return `${secs}s`;
}

export function shortId(id: string): string {
  return id.slice(0, 8);
}

export function formatLatency(ms: number): string {
  return `${ms} ms`;
}

/** "812 B", "4.2 KB", "1.3 MB" (binary units). */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

const timeFormat = new Intl.DateTimeFormat('en', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
});
const dateFormat = new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric' });

/** Time of day for today, "date time" otherwise. */
export function formatTimestamp(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return NONE;
  const time = timeFormat.format(date);
  return date.toDateString() === new Date().toDateString()
    ? time
    : `${dateFormat.format(date)} ${time}`;
}

const relativeFormat = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'short' });

const UNITS: ReadonlyArray<[Intl.RelativeTimeFormatUnit, number]> = [
  ['day', 86_400],
  ['hour', 3_600],
  ['minute', 60],
];

/** "3 min. ago", "yesterday"; falls back to "now" for under a minute. */
export function formatRelative(iso: string | undefined): string {
  if (!iso) return NONE;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return NONE;
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  for (const [unit, size] of UNITS) {
    if (Math.abs(seconds) >= size) return relativeFormat.format(Math.round(seconds / size), unit);
  }
  return relativeFormat.format(0, 'second');
}
