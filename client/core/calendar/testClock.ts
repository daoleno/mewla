import { dateFormat } from "./format";
import type { CalendarClock } from "./model";
import { intlZoneTable, zoneWindow, Zones } from "./zones";

/** A clock for tests: the zone table built from Intl, as a browser builds it. */
export function testClock(now: number | string, device = "UTC", extra: string[] = [], hour12 = true): CalendarClock {
  const at = typeof now === "string" ? Date.parse(now) : now;
  const { fromMs, toMs } = zoneWindow(at);
  const ids = [...new Set(["UTC", "Asia/Shanghai", "America/New_York", "America/Los_Angeles", device, ...extra])];
  const zones = new Zones({ ...intlZoneTable(ids, fromMs, toMs, device), hour12 });
  return { zones, format: dateFormat(zones), now: at };
}
