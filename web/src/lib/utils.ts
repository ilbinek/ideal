import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** Format an ISO timestamp; treat the Go zero-time (year <= 1) as "never". */
export function formatDate(iso: string | undefined): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime()) || d.getFullYear() <= 1) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Format byte counts as MiB/GiB for process memory display. */
export function formatBytes(bytes: number | undefined): string {
  if (bytes == null || !Number.isFinite(bytes) || bytes < 0) return "—";
  const gib = bytes / (1024 * 1024 * 1024);
  if (gib >= 1) return `${gib.toFixed(2)} GiB`;
  const mib = bytes / (1024 * 1024);
  return `${mib.toFixed(0)} MiB`;
}

/** Format process CPU percent (may exceed 100 on multi-core). */
export function formatCpuPercent(pct: number | undefined): string {
  if (pct == null || !Number.isFinite(pct)) return "—";
  return `${pct < 10 ? pct.toFixed(1) : Math.round(pct)}%`;
}
