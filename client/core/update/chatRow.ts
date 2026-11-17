import type { DaemonUpdateStatus } from "../settings/computer";
import { compareVersions } from "./version";

/**
 * The first Mewla whose own update does not wait for Workers (it waits at most an hour for a
 * quiet point and says what for). An older computer installs only once no Worker is running, so a
 * busy one never does: the row offers it at once.
 */
export const BOUNDED_UPDATE_SINCE = "0.4.15";

/** The row at the foot of Brain's Chat about this computer's Mewla, and its one tap. */
export interface ChatUpdateRow {
  key: "update" | "terminals";
  title: string;
  detail?: string;
  button: string;
  action: "update" | "terminals";
}

/**
 * An update that is waiting is never silent: "Update to 0.4.15 waiting for a model reply" (or,
 * on a computer from before bounded updates, "waiting for Workers to finish") with Update now;
 * terminals too old for this Mewla, "Restart terminals to finish updating" with what a restart
 * now ends. Nothing while an update is under way, failed (the computer page says why), or about
 * to install by itself.
 */
export function chatUpdateRow(update: DaemonUpdateStatus | null): ChatUpdateRow | null {
  if (!update || update.updating) return null;
  if (update.terminals_restart) {
    const count = update.live_terminals ?? 0;
    return {
      key: "terminals",
      title: "Restart terminals to finish updating",
      ...(count > 0 ? { detail: `Ends ${count} open ${count === 1 ? "terminal" : "terminals"}` } : {}),
      button: "Restart",
      action: "terminals",
    };
  }
  if (!update.supported || !update.latest || update.error) return null;
  if (update.waiting_for) {
    return { key: "update", title: `Update to ${update.latest} waiting for ${update.waiting_for}`, button: "Update now", action: "update" };
  }
  if (compareVersions(update.version, BOUNDED_UPDATE_SINCE) === -1) {
    return { key: "update", title: `Update to ${update.latest} waiting for Workers to finish`, button: "Update now", action: "update" };
  }
  return null;
}
