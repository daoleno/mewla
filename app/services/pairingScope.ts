export type PairingDevice = "browser" | "phone";

/** Name the device being paired: the browser on web, the phone in the app. */
export function pairingScopeCopy(device: PairingDevice): { title: string; message: string } {
  return {
    title: `Pair this ${device}?`,
    message: `Pairing gives this ${device} access to this Mewla server, including sessions, terminal, Brain, Workers, and files. Only pair a ${device} you trust.`,
  };
}

export class PairingCancelledError extends Error {
  constructor() { super("Pairing cancelled."); this.name = "PairingCancelledError"; }
}

type DaemonIdentity = { daemonId?: string; daemonPublicKey: string };

/**
 * A code from a server this device already saved re-pairs the same device
 * key: the daemon keeps one device for it, so there is no new access to
 * confirm. `mewla open` relies on this to reopen a paired browser quietly.
 */
export function alreadyPairedWith(
  servers: readonly DaemonIdentity[],
  target: DaemonIdentity | null,
): boolean {
  const key = target?.daemonPublicKey.trim().toLowerCase();
  if (!key) return false;
  const id = target?.daemonId?.trim().toLowerCase() || "";
  return servers.some(
    (server) =>
      server.daemonPublicKey.trim().toLowerCase() === key &&
      (!id || server.daemonId?.trim().toLowerCase() === id),
  );
}
