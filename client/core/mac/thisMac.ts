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

/**
 * This Mac's state words in the pane (after "This Mac · "): why the app could not join its own Mewla, else, when its
 * loopback socket finds nothing to answer, that Mewla is not running here, never a bare "Offline" (owner 11-23). A
 * loopback address has no network, DNS or relay in the way, so unreachable or refused can only mean that.
 */
export function thisMacWords(input: { kind: string; diagnosis?: string; problem: string; words: string }): string {
  if (input.problem) return input.problem;
  const unreachable = !input.diagnosis || input.diagnosis === "Server unreachable" || input.diagnosis === "Daemon refused connection";
  return input.kind === "offline" && unreachable ? NOT_RUNNING : input.words;
}

/** What the Mac app says when nothing answers at this Mac's own loopback address. */
export const NOT_RUNNING = "Mewla is not running";

/**
 * This Mac's row in the computers list while another computer is current, also before its entry exists (owner
 * 11-23: with this Mac removed and manjaro current, nothing said that this Mac's Mewla did not answer). The join's
 * own error first; before the join has given it an entry, it is starting; then the probe's word, where a loopback
 * computer that is offline can only be one whose Mewla is not running.
 */
export function thisMacRowWords(input: { problem: string; joined: boolean; word: string }): string {
  if (input.problem) return input.problem;
  if (!input.joined) return "Starting…";
  return input.word === "Offline" || input.word === "Can't connect" ? NOT_RUNNING : input.word;
}
