/**
 * Dates and numbers in English, in the device's own time, without `Intl`: the
 * app's engine (QuickJS) formats `Intl` differently from Bun and browsers, and
 * product text is English whatever the device language ("Oct 27, 8:05 PM").
 */

const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

type DateInput = Date | number | string;

function toDate(value: DateInput): Date | null {
  const date = value instanceof Date ? value : new Date(value);
  return Number.isFinite(date.getTime()) ? date : null;
}

/** "2026-10-27" as that local day, or null. */
export function parseDateOnly(value: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
  if (!match) return null;
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  return date.getMonth() === Number(match[2]) - 1 ? date : null;
}

/** "YYYY-MM-DD" of a local day. */
export function dateKey(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

/** "8:05 PM" */
export function formatTime(value: DateInput): string {
  const date = toDate(value);
  if (!date) return "";
  const hours = date.getHours();
  return `${hours % 12 || 12}:${String(date.getMinutes()).padStart(2, "0")} ${hours < 12 ? "AM" : "PM"}`;
}

/** "Oct 27" */
export function formatShortDate(value: DateInput): string {
  const date = toDate(value);
  return date ? `${MONTHS[date.getMonth()].slice(0, 3)} ${date.getDate()}` : "";
}

/** "Oct 27, 2026" */
export function formatDate(value: DateInput): string {
  const date = toDate(value);
  return date ? `${formatShortDate(date)}, ${date.getFullYear()}` : "";
}

/** "Oct 27, 8:05 PM" */
export function formatDateTime(value: DateInput): string {
  const date = toDate(value);
  return date ? `${formatShortDate(date)}, ${formatTime(date)}` : "";
}

/** "Oct 27, 2026, 8:05 PM" */
export function formatFullDateTime(value: DateInput): string {
  const date = toDate(value);
  return date ? `${formatDate(date)}, ${formatTime(date)}` : "";
}

/** "Sunday, October 27, 2026" */
export function formatLongDay(value: DateInput): string {
  const date = toDate(value);
  return date ? `${WEEKDAYS[date.getDay()]}, ${MONTHS[date.getMonth()]} ${date.getDate()}, ${date.getFullYear()}` : "";
}

/** "1,234,567": digits grouped by three. */
export function groupDigits(value: number, fractionDigits = 0): string {
  const fixed = Math.abs(value).toFixed(fractionDigits);
  const [whole, fraction] = fixed.split(".");
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  return `${value < 0 ? "-" : ""}${grouped}${fraction ? `.${fraction}` : ""}`;
}

/** "$1,234.50", "$0.001234": US dollars, at least two and at most `maxFraction` decimals. */
export function formatUSD(value: number, maxFraction = 2): string {
  const fixed = Math.abs(value).toFixed(Math.max(2, maxFraction));
  const [whole, fraction = ""] = fixed.split(".");
  const trimmed = fraction.replace(/0+$/, "").padEnd(2, "0");
  return `${value < 0 ? "-" : ""}$${whole.replace(/\B(?=(\d{3})+(?!\d))/g, ",")}.${trimmed}`;
}
