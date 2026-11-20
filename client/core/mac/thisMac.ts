import type { PairedDevice } from "../settings/computer";

/** A row of This Mac's Phones and browsers (DESIGN-MAC.md §2.4, `w-mac`). */
export interface DeviceRow {
  id: string;
  name: string;
  icon: "phone" | "browser" | "desktop";
  seen: string;
}

/**
 * The phones and browsers paired with this Mac, last seen first: not the other computers (their own group), not
 * this app itself.
 */
export function phonesAndBrowsers(devices: readonly PairedDevice[], ownId: string, now: Date): DeviceRow[] {
  return devices
    .filter((device) => device.kind !== "computer" && device.id !== ownId)
    .sort((a, b) => Date.parse(b.last_seen_at) - Date.parse(a.last_seen_at))
    .map((device) => ({
      id: device.id,
      name: device.name || "Device",
      icon: device.app_platform === "web" ? "browser" : device.app_platform === "desktop" ? "desktop" : "phone",
      seen: seenWords(device.last_seen_at, now),
    }));
}

/** "Now" within five minutes, the time today, "Yesterday", else the day ("Nov 12"). */
export function seenWords(iso: string, now: Date): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime()) || at.getTime() <= 0) return "";
  if (now.getTime() - at.getTime() < 5 * 60_000) return "Now";
  const day = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  const days = Math.round((day(now) - day(at)) / 86_400_000);
  if (days === 0) return `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;
  if (days === 1) return "Yesterday";
  return at.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}
