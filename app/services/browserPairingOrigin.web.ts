import { normalizeDaemonId, normalizePublicKeyHex } from "./deviceAuthContract";
import type { BrowserPairingTarget } from "./browserPairingOrigin";

export type { BrowserPairingTarget } from "./browserPairingOrigin";

type PageDaemonIdentity = { daemonId: string; daemonPublicKey: string };

/**
 * The daemon allows the page to connect only to its own origin (CSP
 * connect-src, no cross-origin /pair). A link for the daemon serving this page
 * pairs and connects through the page origin; enrollment still verifies the
 * link's daemon key.
 */
export async function resolveBrowserPairingURL(
  target: BrowserPairingTarget,
  pageOrigin: string = window.location.origin,
): Promise<string> {
  const linkOrigin = httpOrigin(target.url);
  if (!linkOrigin || linkOrigin === pageOrigin) {
    return target.url;
  }

  const page = await readPageDaemonIdentity(pageOrigin);
  if (!page) {
    return target.url;
  }

  const linkDaemonId = normalizeDaemonId(target.daemonId);
  if (
    page.daemonPublicKey === normalizePublicKeyHex(target.daemonPublicKey) &&
    (!linkDaemonId || page.daemonId === linkDaemonId)
  ) {
    const ws = new URL("/ws", pageOrigin);
    ws.protocol = ws.protocol === "https:" ? "wss:" : "ws:";
    return ws.toString();
  }

  const linkHost = new URL(linkOrigin).host;
  throw new Error(
    `This link pairs with ${linkHost}, a different Zen daemon from the one serving this page. ` +
      `A browser can only pair with the daemon that serves it. Open ${linkOrigin}/ if that daemon serves the web UI, ` +
      `or run \`zen web\` on that computer.`,
  );
}

function httpOrigin(serverURL: string): string | null {
  try {
    const parsed = new URL(serverURL.trim());
    if (parsed.protocol === "ws:") parsed.protocol = "http:";
    else if (parsed.protocol === "wss:") parsed.protocol = "https:";
    else if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null;
    return parsed.origin;
  } catch {
    return null;
  }
}

async function readPageDaemonIdentity(
  pageOrigin: string,
): Promise<PageDaemonIdentity | null> {
  try {
    const response = await fetch(new URL("/health", pageOrigin).toString(), {
      cache: "no-store",
    });
    if (!response.ok) return null;
    const payload = (await response.json()) as {
      daemon_id?: string;
      daemon_public_key?: string;
    };
    const daemonId = normalizeDaemonId(payload.daemon_id);
    const daemonPublicKey = normalizePublicKeyHex(payload.daemon_public_key);
    return daemonId && daemonPublicKey ? { daemonId, daemonPublicKey } : null;
  } catch {
    return null;
  }
}
