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

/** 0–1 fill for CPU gauge: 1.0 = all logical cores fully busy. */
export function cpuGaugeFill(cpuPercent: number, cpuCount: number): number {
  const cores = cpuCount > 0 ? cpuCount : 1;
  return Math.min(1, Math.max(0, cpuPercent / (100 * cores)));
}

/** 0–1 fill for RAM gauge vs host total memory. */
export function memoryGaugeFill(used: number, total: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return 0;
  return Math.min(1, Math.max(0, used / total));
}

/** Process RAM as percent of host total. */
export function formatMemoryOfHost(used: number, total: number): string {
  if (!Number.isFinite(used) || !Number.isFinite(total) || total <= 0) return "—";
  const pct = (used / total) * 100;
  return `${pct < 10 ? pct.toFixed(1) : Math.round(pct)}%`;
}

/** Format a throughput in bytes/sec (e.g. network rates). */
export function formatBytesPerSec(bps: number | undefined): string {
  if (bps == null || !Number.isFinite(bps) || bps < 0) return "—";
  if (bps >= 1024 * 1024 * 1024) return `${(bps / (1024 * 1024 * 1024)).toFixed(2)} GiB/s`;
  if (bps >= 1024 * 1024) return `${(bps / (1024 * 1024)).toFixed(1)} MiB/s`;
  if (bps >= 1024) return `${(bps / 1024).toFixed(0)} KiB/s`;
  return `${Math.round(bps)} B/s`;
}
