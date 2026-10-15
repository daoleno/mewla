import { parseConnectLink } from "./connection";
import {
  markOnboarded,
  saveServer,
  setServerAutoConnect,
  type StoredServer,
} from "./storage";
import {
  confirmPairedDevice,
  enrollWithDaemon,
  LINK_UNREACHABLE_MESSAGE,
  PairingCodeRejectedError,
  PairingUnreachableError,
  PAIRING_CODE_USED_MESSAGE,
} from "./pairing";
import { resolveBrowserPairingURL } from "./browserPairingOrigin";
import { confirmPairingScope } from "./confirmPairingScope";
import { PairingCancelledError } from "./pairingScope";
import {
  releasePairingConfirmTransport,
  releasePairingLinkTransport,
  resolvePairingConfirmURL,
  resolvePairingLinkURL,
} from "./pinnedTransport";

export interface ImportConnectionOptions {
  onImported?: (server: StoredServer) => void | Promise<void>;
  confirmScope?: () => Promise<boolean>;
  // User-facing progress for the pairing hops, e.g. "Connecting to … through Mewla".
  onProgress?: (message: string) => void;
}

export async function importConnection(
  rawValue: string,
  options: ImportConnectionOptions = {},
): Promise<StoredServer | null> {
  const payload = parseConnectLink(rawValue);
  if (!payload) {
    return null;
  }

  if (!payload.daemonPublicKey || !payload.enrollmentToken) {
    return null;
  }

  if (!await (options.confirmScope ?? confirmPairingScope)()) throw new PairingCancelledError();

  const primaryLinkURL = payload.link
    ? payload.link.candidates.find(
        (candidate) => candidate.admissionUrl === payload.url,
      )?.url ||
      payload.link.candidates[0]?.url ||
      payload.url
    : "";
  if (payload.link) {
    options.onProgress?.(
      `Connecting to ${linkDisplayName(payload.name, primaryLinkURL)} through Mewla…`,
    );
  }
  let pairing: Awaited<ReturnType<typeof enrollWithDaemon>>;
  let pairingURL = "";
  if (payload.link) {
    pairing = await pairOverLink(payload, primaryLinkURL, options);
  } else {
    pairingURL = await resolveBrowserPairingURL({
      url: payload.url,
      daemonId: payload.daemonId,
      daemonPublicKey: payload.daemonPublicKey,
    });
    pairing = await enrollWithDaemon({
      serverUrl: pairingURL,
      daemonId: payload.daemonId,
      daemonPublicKey: payload.daemonPublicKey,
      enrollmentToken: payload.enrollmentToken,
      transport: "direct",
    });
  }

  const primaryStableURL = payload.link ? primaryLinkURL : pairingURL;
  const savedServer = await saveServer({
    name: payload.name || "",
    url: primaryStableURL,
    daemonId: pairing.daemonId,
    daemonPublicKey: pairing.daemonPublicKey,
    transportKind: payload.link ? "link" : "manual",
    transportPin: payload.link?.transportPin,
    linkRouteId: payload.link?.routeId,
    transportCandidates: payload.link?.candidates.map((candidate) => ({
      name: candidate.name,
      kind: "link" as const,
      url: candidate.url,
    })),
  });

  await markOnboarded();
  await setServerAutoConnect(savedServer.id, true);
  await options.onImported?.(savedServer);
  return savedServer;
}

type ImportPayload = NonNullable<ReturnType<typeof parseConnectLink>>;

// pairOverLink spends the one-time admission on /pair. When that answer never
// arrives (stream reset, network drop, a repeated scan of a spent code), the
// daemon may already trust this phone, so a signed probe over the stable
// route decides between "already paired" and the real failing hop.
async function pairOverLink(
  payload: ImportPayload,
  stableURL: string,
  options: ImportConnectionOptions,
): Promise<Awaited<ReturnType<typeof enrollWithDaemon>>> {
  const link = payload.link!;
  let pairError: unknown;
  try {
    const pairingURL = await resolvePairingLinkURL({
      routeId: link.routeId,
      transportPin: link.transportPin,
      url: payload.url,
    });
    return await enrollWithDaemon({
      serverUrl: pairingURL,
      daemonId: payload.daemonId,
      daemonPublicKey: payload.daemonPublicKey,
      enrollmentToken: payload.enrollmentToken,
      transport: "link",
    });
  } catch (error) {
    if (
      !(error instanceof PairingUnreachableError) &&
      !(error instanceof PairingCodeRejectedError)
    ) {
      throw error;
    }
    pairError = error;
  } finally {
    await releasePairingLinkTransport(link.routeId);
  }

  options.onProgress?.("Checking whether this phone is already paired…");
  let confirmed: Awaited<ReturnType<typeof confirmPairedDevice>>;
  try {
    const confirmURL = await resolvePairingConfirmURL({
      routeId: link.routeId,
      transportPin: link.transportPin,
      url: stableURL,
    });
    confirmed = await confirmPairedDevice({
      serverUrl: confirmURL,
      daemonId: payload.daemonId || "",
      daemonPublicKey: payload.daemonPublicKey,
    });
  } catch (error) {
    if (error instanceof PairingUnreachableError) {
      throw new Error(LINK_UNREACHABLE_MESSAGE);
    }
    throw error;
  } finally {
    await releasePairingConfirmTransport(link.routeId);
  }
  if (confirmed) {
    return confirmed;
  }
  // The computer answered and does not trust this phone: the code was spent
  // by another attempt or expired, or the stream broke before the daemon
  // consumed it. Either way only a fresh code can pair.
  throw new Error(
    pairError instanceof PairingCodeRejectedError
      ? PAIRING_CODE_USED_MESSAGE
      : "Pairing didn't finish. Run mewla pair on the computer and scan the new code.",
  );
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
