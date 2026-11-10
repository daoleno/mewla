import type { Zones } from "./zones";

/**
 * Dates in English whatever the phone's language ("Tue, Oct 27", "8:30 PM"), in a zone,
 * from the zone table: QuickJS has no Intl to do it. 12- or 24-hour as the phone shows times.
 */
const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

export interface DateFormat {
  /** "8:30 PM" / "20:30"; `zoneName` adds " PDT". */
  time(at: number, zone: string, zoneName?: boolean): string;
  /** "Oct 28" */
  shortDate(at: number, zone: string): string;
  /** "Tue, Oct 27" */
  day(at: number, zone: string): string;
  /** "Tuesday, October 27" */
  longDay(at: number, zone: string): string;
  /** "Oct 28, 8:30 PM" (+ zone name) */
  dateTime(at: number, zone: string, zoneName?: boolean): string;
  /** "Tuesday, October 27, 2026, 8:30 PM GMT+8": what an edit will save, spelled out. */
  full(at: number, zone: string): string;
  /** "Monday" */
  weekday(at: number, zone: string): string;
}

export function dateFormat(zones: Zones): DateFormat {
  const time = (at: number, zone: string, zoneName = false) => {
    const c = zones.wallClock(at, zone);
    const minute = String(c.minute).padStart(2, "0");
    const text = zones.hour12 ? `${c.hour % 12 || 12}:${minute} ${c.hour < 12 ? "AM" : "PM"}` : `${String(c.hour).padStart(2, "0")}:${minute}`;
    return zoneName ? `${text} ${zones.name(zone, at)}` : text;
  };
  const shortDate = (at: number, zone: string) => {
    const c = zones.wallClock(at, zone);
    return `${MONTHS[c.month - 1].slice(0, 3)} ${c.day}`;
  };
  const longDay = (at: number, zone: string) => {
    const c = zones.wallClock(at, zone);
    return `${WEEKDAYS[c.weekday]}, ${MONTHS[c.month - 1]} ${c.day}`;
  };
  return {
    time,
    shortDate,
    day: (at, zone) => `${WEEKDAYS[zones.wallClock(at, zone).weekday].slice(0, 3)}, ${shortDate(at, zone)}`,
    longDay,
    dateTime: (at, zone, zoneName = false) => `${shortDate(at, zone)}, ${time(at, zone, zoneName)}`,
    full: (at, zone) => `${longDay(at, zone)}, ${zones.wallClock(at, zone).year}, ${time(at, zone, true)}`,
    weekday: (at, zone) => WEEKDAYS[zones.wallClock(at, zone).weekday],
  };
}

/** "October 2026", for a month (no zone: the month grid is wall-clock days). */
export function monthTitle(year: number, month: number): string {
  return `${MONTHS[month - 1]} ${year}`;
}
