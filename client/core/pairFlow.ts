import { parseConnectLink, type ConnectionImportPayload } from "./connection";
import { enrollWithDaemon, PairingCodeRejectedError, PairingUnreachableError } from "./pairing";
import type { StoredServerInput } from "./storedServerContract";

/** A link this build cannot pair through (Mewla Link's pinned tunnel, until the hosts carry it). */
export class PairingLinkError extends Error {}

/** Reads a pasted, scanned or opened pairing link; null when the text is not one. */
export function readPairingLink(text: string): ConnectionImportPayload | null {
  const payload = parseConnectLink(text.trim());
  return payload?.daemonPublicKey && payload.enrollmentToken ? payload : null;
}

/** What the confirmation asks: pairing grants this phone the computer's sessions, terminal, Brain, Workers and files. */
export function pairingScopeCopy(device: "phone" | "browser" = "phone"): { title: string; message: string; confirm: string } {
  return {
    title: `Pair this ${device}?`,
    message: `Pairing gives this ${device} access to this Mewla server, including sessions, terminal, Brain, Workers, and files. Only pair a ${device} you trust.`,
    confirm: "Pair and grant access",
  };
}

type DaemonIdentity = { daemonId?: string; daemonPublicKey: string };

/**
 * A code from a computer this phone already paired re-pairs the same device key: the daemon keeps
 * one device for it, so there is no new access to confirm.
 */
export function alreadyPairedWith(servers: readonly DaemonIdentity[], target: DaemonIdentity | null): boolean {
  const key = target?.daemonPublicKey.trim().toLowerCase();
  if (!key) return false;
  const id = target?.daemonId?.trim().toLowerCase() || "";
  return servers.some((server) => server.daemonPublicKey.trim().toLowerCase() === key && (!id || server.daemonId?.trim().toLowerCase() === id));
}

/** What the person reads when a code does not pair: a title and the reason. */
export interface PairingProblem {
  title: string;
  detail: string;
}

export const NOT_A_CODE: PairingProblem = {
  title: "Not a pairing code",
  detail: "Scan the QR code mewla shows, or paste the link from mewla pair --details.",
};

export const NOTHING_TO_PASTE: PairingProblem = {
  title: "Nothing to paste",
  detail: "Copy the link from mewla pair --details on your computer, then tap Paste again.",
};

export function pairingProblem(error: unknown): PairingProblem {
  const detail = error instanceof Error && error.message.trim() ? error.message.trim() : "Could not pair with that computer.";
  if (error instanceof PairingCodeRejectedError) return { title: "Code used or expired", detail };
  if (error instanceof PairingUnreachableError) return { title: "Can't reach your computer", detail };
  return { title: "Pairing failed", detail };
}

/**
 * Pairs this device with the computer a link names and returns the server to keep.
 * Links through Mewla Link need the host's pinned tunnel, which this build does not carry yet;
 * they are refused with a clear message.
 */
export async function pairWith(payload: ConnectionImportPayload): Promise<StoredServerInput> {
  if (payload.link) throw new PairingLinkError("This code pairs through Mewla Link, which this build can't use yet. On the computer, run mewla pair with an address this phone reaches directly (mewla pair --help shows how).");
  const pairing = await enrollWithDaemon({
    serverUrl: payload.url,
    daemonId: payload.daemonId,
    daemonPublicKey: payload.daemonPublicKey,
    enrollmentToken: payload.enrollmentToken,
    transport: "direct",
  });
  return {
    name: payload.name || "",
    url: payload.url,
    daemonId: pairing.daemonId,
    daemonPublicKey: pairing.daemonPublicKey,
    transportKind: "manual",
  };
}
