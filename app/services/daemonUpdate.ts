import { getOrCreateLocalDeviceIdentity, type LocalDeviceIdentity } from "./auth";
import type { StoredServer } from "./storage";
import { requestFailure, signedFetch } from "./signedRequest";

/** What a computer reports about its own Mewla. */
export interface DaemonUpdateStatus {
  /** The Mewla running on the computer. */
  version: string;
  /** False for builds that do not update themselves (development builds, betas). */
  supported: boolean;
  /** The newer stable release, when there is one. */
  latest?: string;
  /** The computer is downloading and restarting into `latest`. */
  updating?: boolean;
  /** Why the last update did not take; the previous version kept running. */
  error?: string;
}

/**
 * The computer's version and update, or null when it is too old to report one
 * (it predates self-update and has no such endpoint).
 */
export async function getDaemonUpdate(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<DaemonUpdateStatus | null> {
  const identity = await loadIdentity();
  const response = await signedFetch(server, "GET", "/daemon-update", "mewla-daemon-update:GET:/daemon-update", undefined, identity);
  if (response.status === 404 || response.status === 503) return null;
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as DaemonUpdateStatus;
}

/** Updates the computer now: it downloads, verifies, restarts, and keeps the old version if the new one will not start. */
export async function installDaemonUpdate(
  server: StoredServer,
  loadIdentity: () => Promise<LocalDeviceIdentity> = getOrCreateLocalDeviceIdentity,
): Promise<DaemonUpdateStatus> {
  const identity = await loadIdentity();
  const response = await signedFetch(server, "POST", "/daemon-update", "mewla-daemon-update:POST:/daemon-update", undefined, identity);
  if (!response.ok) throw await requestFailure(server, response);
  return (await response.json()) as DaemonUpdateStatus;
}
