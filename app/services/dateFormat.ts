/**
 * Every date and time Mewla shows. Product text is English, so dates are too,
 * whatever the device language: "Tue, Oct 27", "8:05 PM", never "10月27日星期二".
 * Pure (no react-native) so services and tests can use it.
 */
export const APP_LOCALE = "en-US";

type DateInput = Date | number | string;

/** Named styles, so the same kind of date reads the same everywhere. */
export const DateStyle = {
  /** "8:05 PM" */
  time: { hour: "numeric", minute: "2-digit" },
  /** "Oct 27" */
  shortDate: { month: "short", day: "numeric" },
  /** "Tue, Oct 27" */
  day: { weekday: "short", month: "short", day: "numeric" },
  /** "October 27" */
  longDate: { month: "long", day: "numeric" },
  /** "Tuesday, October 27" */
  longDay: { weekday: "long", month: "long", day: "numeric" },
  /** "Tue" */
  weekday: { weekday: "short" },
  /** "October 2026" */
  monthYear: { month: "long", year: "numeric" },
  /** "Oct 27, 8:05 PM" */
  dateTime: { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" },
  /** "Oct 27, 2026, 8:05 PM" */
  fullDateTime: { month: "short", day: "numeric", year: "numeric", hour: "numeric", minute: "2-digit" },
} as const satisfies Record<string, Intl.DateTimeFormatOptions>;

const formatters = new Map<string, Intl.DateTimeFormat>();

export function dateFormatter(options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  const key = JSON.stringify(options);
  let formatter = formatters.get(key);
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(APP_LOCALE, options);
    formatters.set(key, formatter);
  }
  return formatter;
}

/**
 * Formats a date in English. Extra options (a time zone, a time zone name,
 * the year) extend the style. An invalid date formats as "".
 */
export function formatDate(
  value: DateInput,
  style: Intl.DateTimeFormatOptions = DateStyle.dateTime,
  extra?: Intl.DateTimeFormatOptions,
): string {
  const date = value instanceof Date ? value : new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  return dateFormatter(extra ? { ...style, ...extra } : style).format(date);
}

/** Counts, money and other numbers in the same English style. */
export function formatNumber(value: number, options?: Intl.NumberFormatOptions): string {
  return value.toLocaleString(APP_LOCALE, options);
}
