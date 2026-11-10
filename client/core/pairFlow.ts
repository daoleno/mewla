import { parseConnectLink, type ConnectionImportPayload } from "./connection";
import { enrollWithDaemon } from "./pairing";
import type { StoredServerInput } from "./storedServerContract";

export class PairingLinkError extends Error {}

/** Reads a pasted or scanned pairing link; null when the text is not one. */
export function readPairingLink(text: string): ConnectionImportPayload | null {
  return parseConnectLink(text);
}

/**
 * Pairs this device with the computer a link names and returns the server to keep.
 * Links through Mewla Link need the host's pinned tunnel, which the app's next
 * milestone brings; until then they are refused with a clear message.
 */
export async function pairWith(payload: ConnectionImportPayload): Promise<StoredServerInput> {
  if (payload.link) throw new PairingLinkError("This preview pairs over a direct address only. Run mewla pair with the computer's address.");
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
