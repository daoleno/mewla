import { parseConnectLink, type ConnectionImportPayload } from "./connection";
import {
  releasePairingConfirmTransport,
  releasePairingLinkTransport,
  resolvePairingConfirmURL,
  resolvePairingLinkURL,
} from "./link/pinnedTransport";
import {
  confirmPairedDevice,
  enrollWithDaemon,
  LINK_UNREACHABLE_MESSAGE,
  PAIRING_CODE_USED_MESSAGE,
  PairingCodeRejectedError,
  PairingUnreachableError,
} from "./pairing";
import type { StoredServerInput } from "./storedServerContract";

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

/**
 * The computers a browser just paired over supersedes (R5#1): entries at the same address with
 * another daemon key. A page is served by the one computer at its origin, so such an entry is that
 * address's old install (mewla reinstalled, a new state dir): it is refused for ever
 * (`wrong-daemon`) and only lists a second, dead computer of the same name. Phones keep them:
 * a LAN address can pass from one computer to another.
 */
export function supersededAtAddress<T extends DaemonIdentity & { id: string; url: string }>(servers: readonly T[], paired: T): string[] {
  const address = paired.url.trim().toLowerCase();
  const key = paired.daemonPublicKey.trim().toLowerCase();
  return servers
    .filter((server) => server.id !== paired.id && server.url.trim().toLowerCase() === address && server.daemonPublicKey.trim().toLowerCase() !== key)
    .map((server) => server.id);
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

/**
 * The system refused to hand over the clipboard (iOS's "Don't Allow Paste", or Paste from Other Apps set to Deny:
 * PBErrorDomain 13, "Operation not authorized"). It is not empty: say it was refused, with the system's words.
 */
export function pasteRefused(error: unknown): PairingProblem {
  const reason = error instanceof Error ? error.message.trim().replace(/\.$/, "") : "";
  return {
    title: "Paste wasn't allowed",
    detail: `${reason ? `${reason}. ` : ""}Tap Paste again and allow it, or type the link.`,
  };
}

/**
 * What a read of the clipboard came to. iOS answers a paste the person denied with no text and no error (NSH 0.5.1
 * reads `UIPasteboard.string`, nil when denied, and resolves ""; longbridge/nsh#2004 rejects it on main, after 0.5.1), while `hasStrings`, which asks nothing, still says
 * text is there: no text from a clipboard that holds some is a refusal (Mac re-run 11-14, 6c: PBErrorDomain 13 in the
 * log, "Nothing to paste" on screen). An error from the read is one too.
 */
export function clipboardRead(read: { text: string; has: boolean } | { error: unknown }): { kind: "text"; text: string } | { kind: "empty" } | { kind: "refused"; error?: unknown } {
  if ("error" in read) return { kind: "refused", error: read.error };
  if (read.text === "" && read.has) return { kind: "refused" };
  const text = read.text.trim();
  return text ? { kind: "text", text } : { kind: "empty" };
}

export const LINK_REQUIRED: PairingProblem = {
  title: "Pairing link required",
  detail: "Paste the link mewla pair --details prints, or scan its QR code.",
};

export const NO_CODE_IN_PICTURE: PairingProblem = {
  title: "No QR code found",
  detail: "Pick a screenshot or photo of the code mewla pair shows.",
};

export function pairingProblem(error: unknown): PairingProblem {
  const detail = error instanceof Error && error.message.trim() ? error.message.trim() : "Could not pair with that computer.";
  if (error instanceof PairingCodeRejectedError) return { title: "Code used or expired", detail };
  if (error instanceof PairingUnreachableError) return { title: "Can't reach your computer", detail };
  return { title: "Pairing failed", detail };
}

/**
 * Pairs this device with the computer a link names and returns the server to keep. A Mewla Relay
 * code pairs through the host's pinned tunnel to the relay and keeps the route, pin and
 * candidates, so every later connection goes the same way (core/link/pinnedTransport.ts).
 * `onProgress` hears what a slow Link pairing is doing.
 */
export async function pairWith(payload: ConnectionImportPayload, onProgress?: (message: string) => void): Promise<StoredServerInput> {
  if (payload.link) return pairThroughLink(payload, payload.link, onProgress);
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

async function pairThroughLink(
  payload: ConnectionImportPayload,
  link: NonNullable<ConnectionImportPayload["link"]>,
  onProgress?: (message: string) => void,
): Promise<StoredServerInput> {
  // The stable URL every later connection uses: the candidate whose admission this code names.
  const stableURL = link.candidates.find((candidate) => candidate.admissionUrl === payload.url)?.url || link.candidates[0]?.url || payload.url;
  onProgress?.(`Connecting to ${linkDisplayName(payload.name, stableURL)} through Mewla…`);
  const pairing = await pairOverLink(payload, link, stableURL, onProgress);
  return {
    name: payload.name || "",
    url: stableURL,
    daemonId: pairing.daemonId,
    daemonPublicKey: pairing.daemonPublicKey,
    transportKind: "link",
    transportPin: link.transportPin,
    linkRouteId: link.routeId,
    transportCandidates: link.candidates.map((candidate) => ({ name: candidate.name, kind: "link" as const, url: candidate.url })),
  };
}

// The one-time admission is spent on /pair. When its answer never arrives (stream reset, network
// drop, a repeated scan of a spent code) the daemon may already trust this phone, so a signed probe
// over the stable route decides between "already paired" and the real failure.
async function pairOverLink(
  payload: ConnectionImportPayload,
  link: NonNullable<ConnectionImportPayload["link"]>,
  stableURL: string,
  onProgress?: (message: string) => void,
): Promise<{ daemonId: string; daemonPublicKey: string }> {
  let pairError: unknown;
  try {
    const pairingURL = await resolvePairingLinkURL({ routeId: link.routeId, transportPin: link.transportPin, url: payload.url });
    return await enrollWithDaemon({
      serverUrl: pairingURL,
      daemonId: payload.daemonId,
      daemonPublicKey: payload.daemonPublicKey,
      enrollmentToken: payload.enrollmentToken,
      transport: "link",
    });
  } catch (error) {
    if (!(error instanceof PairingUnreachableError) && !(error instanceof PairingCodeRejectedError)) throw error;
    pairError = error;
  } finally {
    await releasePairingLinkTransport(link.routeId);
  }

  onProgress?.("Checking whether this phone is already paired…");
  let confirmed: { daemonId: string; daemonPublicKey: string } | null;
  try {
    const confirmURL = await resolvePairingConfirmURL({ routeId: link.routeId, transportPin: link.transportPin, url: stableURL });
    confirmed = await confirmPairedDevice({ serverUrl: confirmURL, daemonId: payload.daemonId || "", daemonPublicKey: payload.daemonPublicKey });
  } catch (error) {
    if (error instanceof PairingUnreachableError) throw new PairingUnreachableError(LINK_UNREACHABLE_MESSAGE);
    throw error;
  } finally {
    await releasePairingConfirmTransport(link.routeId);
  }
  if (confirmed) return confirmed;
  // The computer answered and does not trust this phone: the code was spent by another attempt or
  // expired, or the stream broke before the daemon consumed it. Only a fresh code can pair.
  if (pairError instanceof PairingCodeRejectedError) throw new PairingCodeRejectedError(PAIRING_CODE_USED_MESSAGE);
  throw new Error("Pairing didn't finish. Run mewla pair on the computer and scan the new code.");
}

function linkDisplayName(name: string | undefined, stableURL: string): string {
  const trimmed = name?.trim();
  if (trimmed) return trimmed;
  try {
    return new URL(stableURL).hostname.split(".")[0] || "your computer";
  } catch {
    return "your computer";
  }
}
