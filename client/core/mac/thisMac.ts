import type { PairedDevice } from "../settings/computer";
import { normalizeServerURL, type StoredServer } from "../storedServerContract";

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

/**
 * This Mac's own computer among the stored ones (owner 11-23: "a Mac app always has its own computer"): the one entry
 * for its daemon's key, reached at its own loopback address. `keep` is that entry (the one already at `localURL`, else
 * the current one, else the first), `drop` the other entries for the same key (no second "MacBook Pro"), and `rewrite`
 * whether `keep` must move to `localURL` in place (an old pairing by the hosted name or Mewla Relay: same id, so its
 * Sessions, alerts and name stay). With no entry, `keep` is null: the app pairs its daemon over loopback.
 */
export function ownEntryPlan(
  servers: readonly StoredServer[],
  daemonPublicKey: string,
  localURL: string,
  currentId: string | null,
): { keep: StoredServer | null; drop: string[]; rewrite: boolean } {
  const key = daemonPublicKey.trim().toLowerCase();
  const local = normalizeServerURL(localURL);
  const mine = servers.filter((server) => server.daemonPublicKey.trim().toLowerCase() === key);
  const keep = mine.find((server) => server.url === local) ?? mine.find((server) => server.id === currentId) ?? mine[0] ?? null;
  return {
    keep,
    drop: mine.filter((server) => server !== keep).map((server) => server.id),
    rewrite: Boolean(keep && (keep.url !== local || keep.transportKind === "link" || keep.transportPin || keep.linkRouteId || keep.transportCandidates?.length)),
  };
}

/** What the Mac app says when nothing answers at this Mac's own loopback address. */
export const NOT_RUNNING = "Mewla is not running";

/**
 * Where This Mac stands, as each surface knows it: no entry yet (the join has not made it); its entry the current
 * computer (its connection's state and diagnosis); or its entry beside another current computer (what its own
 * loopback check answered, and the row's word from the current computer's view of it).
 */
export type ThisMacState =
  | { entry: "none" }
  | { entry: "current"; kind: string; diagnosis?: string }
  | { entry: "other"; check?: "online" | "offline" | "unpaired" };

/**
 * This Mac's words wherever it shows (owner 11-23: never a bare "Offline"; the 0.4.27 gate: the row said "Offline ·
 * last seen" with its daemon stopped): the pane after "This Mac · ", its row in the computers list, the window while
 * it starts. One rule for all of them. The join's own reason first (`problem`: the system refusing to start Mewla,
 * `mewla pair`'s error); then, where its loopback address finds nothing to answer, that Mewla is not running (a
 * loopback address has no network, DNS or relay in the way, so unreachable or refused can only mean that); else the
 * surface's own words. Its own check outranks the current computer's view of it, which only says when it last saw it.
 */
export function thisMacSays(input: { problem: string; state: ThisMacState; words: string }): string {
  if (input.problem) return input.problem;
  const state = input.state;
  if (state.entry === "none") return "Starting…";
  if (state.entry === "current") {
    const unreachable = !state.diagnosis || state.diagnosis === "Server unreachable" || state.diagnosis === "Daemon refused connection";
    return state.kind === "offline" && unreachable ? NOT_RUNNING : input.words;
  }
  if (state.check === "offline") return NOT_RUNNING;
  if (state.check === "online") return input.words.startsWith("Online") ? input.words : "Online";
  return /^(Offline|Can't connect)\b/.test(input.words) ? NOT_RUNNING : input.words;
}
