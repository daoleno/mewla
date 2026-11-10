import { computed, host, platform, signal } from "nsh";
import { dateFormat } from "../../core/calendar/format";
import type { CalendarClock } from "../../core/calendar/model";
import { intlZoneTable, zoneWindow, Zones, type ZoneTable } from "../../core/calendar/zones";

/**
 * The time zones the Calendar shows times in. QuickJS has only UTC, so the host gives a
 * table of the zones asked for (`calendar.zones`), a browser builds it from Intl. Asked
 * again when an item brings a zone the table lacks, and every day for the window.
 */
const table = signal<ZoneTable>({ device: "UTC", hour12: true, zones: {} });
const now = signal(Date.now());
setInterval(() => now.set(Date.now()), 30_000);

export const clock = computed<CalendarClock>(() => {
  const zones = new Zones(table());
  return { zones, format: dateFormat(zones), now: now() };
});

let asked = new Set<string>();
let askedAt = 0;
let loading: Promise<void> | null = null;

/** Makes sure the table has these zones (and the phone's); resolves once it has. */
export function ensureZones(ids: readonly string[] = []): Promise<void> {
  const missing = ids.filter((id) => id && !asked.has(id));
  if (loading && missing.length === 0) return loading;
  if (missing.length === 0 && askedAt && Date.now() - askedAt < 86_400_000) return Promise.resolve();
  const wanted = [...new Set([...asked, ...missing, "UTC"])];
  const window = zoneWindow();
  const load = (async () => {
    let next: ZoneTable;
    if (host.available() && platform.os !== "web") {
      next = (await host.call<ZoneTable>("calendar.zones", { zones: wanted, ...window }).catch(() => null)) ?? table();
      // The phone's own zone comes with the answer: ask once more if it was not among them.
      if (next.zones[next.device] === undefined) {
        wanted.push(next.device);
        next = (await host.call<ZoneTable>("calendar.zones", { zones: wanted, ...window }).catch(() => null)) ?? next;
      }
    } else {
      const device = (() => { try { return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"; } catch { return "UTC"; } })();
      next = intlZoneTable([...new Set([...wanted, device])], window.fromMs, window.toMs, device);
    }
    asked = new Set([...wanted, next.device]);
    askedAt = Date.now();
    table.set(next);
  })();
  loading = load.finally(() => {
    if (loading === load) loading = null;
  });
  return load;
}

/** The phone's IANA zone (for the computer's push registration and Watch's quiet hours). */
export async function deviceZone(): Promise<string> {
  await ensureZones();
  return table().device;
}
