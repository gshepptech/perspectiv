/**
 * Place-local calendar days. An arc's day is the calendar day in the place's
 * IANA time zone, never in the author's device zone and never derived from a
 * publish timestamp, so every day here is a plain "YYYY-MM-DD" string.
 */

const LOCAL_DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Today's calendar day in `timeZone` at the instant `now`. */
export function todayIn(timeZone: string, now: Date = new Date()): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const part = (type: Intl.DateTimeFormatPartTypes) => parts.find((p) => p.type === type)?.value ?? "";
  return `${part("year")}-${part("month")}-${part("day")}`;
}

/** True for a real calendar day written as YYYY-MM-DD. */
export function isLocalDate(value: string | null | undefined): value is string {
  if (!value) return false;
  const m = LOCAL_DATE.exec(value);
  if (!m) return false;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const date = new Date(Date.UTC(y, mo - 1, d));
  return date.getUTCFullYear() === y && date.getUTCMonth() === mo - 1 && date.getUTCDate() === d;
}

/** An hour 0 to 23 from a search parameter, or undefined for the day arc. */
export function parseHour(value: string | null | undefined): number | undefined {
  if (value == null || !/^\d{1,2}$/.test(value)) return undefined;
  const hour = Number(value);
  return hour >= 0 && hour <= 23 ? hour : undefined;
}

/** "09:00": the place-local hour an hour arc covers. */
export function hourLabel(hour: number): string {
  return `${String(hour).padStart(2, "0")}:00`;
}
