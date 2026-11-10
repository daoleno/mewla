/**
 * Time zones without Intl. QuickJS knows only UTC, so the host gives the app a table:
 * for each zone it asks about, the offset from the start of a window and every change
 * within it (`calendar.zones`, java.time / Foundation). Everything else (a date key in a
 * zone, a wall-clock time to an instant, the DST gap and overlap) is plain arithmetic
 * here. A browser and `bun test` build the same table from Intl (`intlZoneTable`).
 */
export interface ZoneSpan {
  /** From this instant on (until the next span)… */
  atMs: number;
  /** …local time is UTC plus this. */
  offsetMinutes: number;
  /** Its short English name: "PDT", "GMT+8". */
  name: string;
}

export interface ZoneTable {
  /** The phone's own zone. */
  device: string;
  /** Whether the phone shows 12-hour times. */
  hour12: boolean;
  /** Per zone asked, its spans in order; null for an id that is no zone. */
  zones: Record<string, ZoneSpan[] | null>;
}

export interface WallClock {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
  /** 0 = Sunday. */
  weekday: number;
}

export type LocalResolution =
  | { status: "invalid"; message: string }
  | { status: "gap"; message: string }
  | { status: "ambiguous"; message: string; instants: [number, number] }
  | { status: "resolved"; instant: number };

const MINUTE = 60_000;

export class Zones {
  constructor(readonly table: ZoneTable) {}

  get device(): string {
    return this.table.device;
  }

  get hour12(): boolean {
    return this.table.hour12;
  }

  /** Whether the table knows `zone` (UTC always). */
  knows(zone: string): boolean {
    return zone === "UTC" || !!this.table.zones[zone];
  }

  private span(zone: string, at: number): ZoneSpan {
    const spans = this.table.zones[zone];
    if (!spans || spans.length === 0) return { atMs: 0, offsetMinutes: 0, name: "UTC" };
    let found = spans[0];
    for (const span of spans) {
      if (span.atMs > at) break;
      found = span;
    }
    return found;
  }

  offsetMinutes(zone: string, at: number): number {
    return this.span(zone, at).offsetMinutes;
  }

  /** "PDT", "GMT+8": the zone's short name at that instant. */
  name(zone: string, at: number): string {
    return this.span(zone, at).name;
  }

  wallClock(at: number, zone: string): WallClock {
    const local = new Date(at + this.offsetMinutes(zone, at) * MINUTE);
    return {
      year: local.getUTCFullYear(),
      month: local.getUTCMonth() + 1,
      day: local.getUTCDate(),
      hour: local.getUTCHours(),
      minute: local.getUTCMinutes(),
      second: local.getUTCSeconds(),
      weekday: local.getUTCDay(),
    };
  }

  /**
   * "2026-11-12T10:30:00+08:00": an instant as the computer stores it, with the offset its
   * zone has then. The computer refuses any other offset (a UTC "Z" for Asia/Shanghai).
   */
  iso(at: number, zone: string): string {
    const c = this.wallClock(at, zone);
    const offset = this.offsetMinutes(zone, at);
    const sign = offset < 0 ? "-" : "+";
    const abs = Math.abs(offset);
    return `${this.dateKey(at, zone)}T${pad(c.hour)}:${pad(c.minute)}:${pad(c.second)}${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
  }

  /** "2026-10-27": the date of an instant in a zone; agenda days and month dots use it. */
  dateKey(at: number, zone: string): string {
    const c = this.wallClock(at, zone);
    return `${String(c.year).padStart(4, "0")}-${pad(c.month)}-${pad(c.day)}`;
  }

  /** `{date: "2026-10-27", time: "08:30"}`: what the editor shows for an instant in the item's zone. */
  localFields(at: number, zone: string): { date: string; time: string } {
    const c = this.wallClock(at, zone);
    return { date: this.dateKey(at, zone), time: `${pad(c.hour)}:${pad(c.minute)}` };
  }

  /**
   * A wall-clock date and time in a zone to its instant. A time the clock skips (DST
   * gap) is refused; a time it shows twice (DST overlap) returns both instants, for the
   * person to choose.
   */
  resolveLocal(date: string, time: string, zone: string): LocalResolution {
    const requested = parseLocal(date, time);
    if (!requested) return { status: "invalid", message: "Enter a valid date (YYYY-MM-DD) and time (HH:MM)." };
    const name = zone.trim();
    if (!this.knows(name)) return { status: "invalid", message: "Enter a valid IANA timezone, such as Asia/Shanghai." };
    const wall = Date.UTC(requested.year, requested.month - 1, requested.day, requested.hour, requested.minute);
    const offsets = new Set<number>();
    for (let hours = -36; hours <= 36; hours += 6) offsets.add(this.offsetMinutes(name, wall + hours * 3_600_000));
    const matches = [...offsets]
      .map((offset) => wall - offset * MINUTE)
      .filter((at) => this.offsetMinutes(name, at) * MINUTE === wall - at)
      .sort((a, b) => a - b)
      .filter((value, index, all) => index === 0 || value !== all[index - 1]);
    if (matches.length === 0) {
      return { status: "gap", message: `${date} at ${time} does not exist in ${name} because the clock moves forward. Choose another time.` };
    }
    if (matches.length > 1) {
      return {
        status: "ambiguous",
        message: `${date} at ${time} occurs twice in ${name} because the clock moves back. Choose the first or second occurrence.`,
        instants: [matches[0], matches[matches.length - 1]],
      };
    }
    return { status: "resolved", instant: matches[0] };
  }
}

function pad(value: number): string {
  return String(value).padStart(2, "0");
}

function parseLocal(date: string, time: string) {
  const d = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date.trim());
  const t = /^(\d{1,2}):(\d{2})$/.exec(time.trim());
  if (!d || !t) return null;
  const parts = { year: Number(d[1]), month: Number(d[2]), day: Number(d[3]), hour: Number(t[1]), minute: Number(t[2]) };
  const check = new Date(Date.UTC(parts.year, parts.month - 1, parts.day));
  if (parts.year < 1 || parts.hour > 23 || parts.minute > 59 || check.getUTCMonth() + 1 !== parts.month || check.getUTCDate() !== parts.day) return null;
  return parts;
}

/** The window the app asks the host about: a year back, a year and a half ahead. */
export function zoneWindow(now = Date.now()): { fromMs: number; toMs: number } {
  return { fromMs: now - 400 * 86_400_000, toMs: now + 550 * 86_400_000 };
}

/**
 * The same table from Intl, where Intl knows the zones (a browser, `bun test`): offsets
 * sampled every six hours, each change found to the minute.
 */
export function intlZoneTable(ids: string[], fromMs: number, toMs: number, device?: string): ZoneTable {
  const zones: ZoneTable["zones"] = {};
  for (const id of ids) {
    let offsetAt: (at: number) => number;
    let nameAt: (at: number) => string;
    try {
      const parts = new Intl.DateTimeFormat("en-US", {
        timeZone: id, hourCycle: "h23", year: "numeric", month: "numeric", day: "numeric", hour: "numeric", minute: "numeric",
      });
      const names = new Intl.DateTimeFormat("en-US", { timeZone: id, timeZoneName: "short" });
      offsetAt = (at) => {
        const v = Object.fromEntries(parts.formatToParts(new Date(at)).filter((p) => p.type !== "literal").map((p) => [p.type, Number(p.value)]));
        const flat = Math.floor(at / MINUTE) * MINUTE;
        return Math.round((Date.UTC(v.year, v.month - 1, v.day, v.hour, v.minute) - flat) / MINUTE);
      };
      nameAt = (at) => names.formatToParts(new Date(at)).find((p) => p.type === "timeZoneName")?.value ?? id;
    } catch {
      zones[id] = null;
      continue;
    }
    const spans: ZoneSpan[] = [{ atMs: fromMs, offsetMinutes: offsetAt(fromMs), name: nameAt(fromMs) }];
    const step = 6 * 3_600_000;
    for (let at = fromMs + step; at <= toMs; at += step) {
      const offset = offsetAt(at);
      if (offset === spans[spans.length - 1].offsetMinutes) continue;
      let lo = at - step;
      let hi = at;
      while (hi - lo > MINUTE) {
        const mid = Math.floor((lo + hi) / 2 / MINUTE) * MINUTE;
        if (mid <= lo) break;
        if (offsetAt(mid) === offset) hi = mid;
        else lo = mid;
      }
      spans.push({ atMs: hi, offsetMinutes: offset, name: nameAt(hi) });
    }
    zones[id] = spans;
  }
  const deviceZone = device ?? (Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
  return { device: deviceZone, hour12: true, zones };
}
