import type { RestartSlot } from "./types";

const MINUTES_PER_DAY = 1440;
const ALL_DAYS = 127;

/** A restart time as the person entering it sees it, in their own timezone. */
export interface LocalSlot {
  /** Minute of the day, local. */
  minute: number;
  /** Weekday mask in local time, bit 0 = Sunday. */
  days: number;
}

/** Moves every day in a weekday mask forward (+1) or back (-1) by one day. */
function rotateDays(days: number, by: 1 | -1 | 0): number {
  if (by === 0) return days;
  const rotated = by === 1 ? (days << 1) | (days >> 6) : (days >> 1) | (days << 6);
  return rotated & ALL_DAYS;
}

/**
 * Applies an offset to a time, moving its weekdays when it crosses midnight. A time
 * near midnight can land on a different day in UTC, so each time has its own days.
 */
function shift(minute: number, days: number, by: number): { minute: number; days: number } {
  const moved = minute + by;
  if (moved < 0) return { minute: moved + MINUTES_PER_DAY, days: rotateDays(days, -1) };
  if (moved >= MINUTES_PER_DAY) return { minute: moved - MINUTES_PER_DAY, days: rotateDays(days, 1) };
  return { minute: moved, days };
}

/** Minutes this browser's timezone is ahead of UTC right now, so UTC+1 gives 60. */
export function currentUtcOffset(now: Date = new Date()): number {
  return -now.getTimezoneOffset();
}

/** Converts a stored UTC slot to what to show, at the given offset from UTC. */
export function slotToLocal(slot: RestartSlot, offset: number): LocalSlot {
  const { minute, days } = shift(slot.m, slot.d, offset);
  return { minute, days };
}

/** Converts what was entered to the UTC slot to store, at the given offset from UTC. */
export function localToSlot(local: LocalSlot, offset: number): RestartSlot {
  const { minute, days } = shift(local.minute, local.days, -offset);
  return { m: minute, d: days };
}

/** "06:30" for 390. */
export function formatMinute(minute: number): string {
  const hours = Math.floor(minute / 60);
  const mins = minute % 60;
  return `${String(hours).padStart(2, "0")}:${String(mins).padStart(2, "0")}`;
}

/** 390 for "06:30", or null if it can't be read. */
export function parseMinute(value: string): number | null {
  const match = /^(\d{1,2}):(\d{2})$/.exec(value);
  if (!match) return null;
  const hours = Number(match[1]);
  const mins = Number(match[2]);
  if (hours > 23 || mins > 59) return null;
  return hours * 60 + mins;
}

/** Reads "15, 5, 1" as [15, 5, 1], or null if anything in it isn't a whole number. */
export function parseWarnMinutes(value: string): number[] | null {
  const parts = value
    .split(",")
    .map((p) => p.trim())
    .filter((p) => p !== "");
  const out: number[] = [];
  for (const part of parts) {
    if (!/^\d+$/.test(part)) return null;
    out.push(Number(part));
  }
  return out;
}

export { ALL_DAYS };
