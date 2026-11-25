import { serverURLNow } from "../link/pinnedTransport";
import { signedFetch } from "../more/daemon";
import type { StoredServer } from "../storedServerContract";
import type { SessionFileBinaryRequest } from "./sessionFilePreview";

/**
 * An HTML file a Session wrote, shown as a page: the computer serves it by URL under a grant it signs
 * for the page's folder (daemon/server/session_page.go), sandboxed to an origin of its own, so the
 * page's scripts, its CDN libraries and the files beside it load as a browser would load them.
 */
export interface SessionPage {
  url: string;
  expiresAtMS: number;
}

const PAGE_PREFIX = "/session-page/";

/** The page's URL on the computer as this client reaches it, or null for a path that is not a page grant's. */
export function sessionPageUrl(serverUrl: string, path: string): string | null {
  if (!path.startsWith(PAGE_PREFIX) || path.includes("?") || path.includes("#") || path.includes("\\")) return null;
  try {
    const url = new URL(serverUrl);
    if (url.protocol === "ws:") url.protocol = "http:";
    if (url.protocol === "wss:") url.protocol = "https:";
    if (url.protocol !== "http:" && url.protocol !== "https:") return null;
    const page = new URL(path, url.origin);
    return page.origin === url.origin && page.pathname === path ? page.toString() : null;
  } catch {
    return null;
  }
}

/**
 * Whether the app's own web view shows the page: in a browser always (a frame of the page's own computer), elsewhere
 * any page on http(s). A host `webview.hosts` grants (nsh.json: `127.0.0.1`, where a computer paired through Mewla
 * Relay and Mewla for Mac's own are reached) shows as it is; the page of a computer paired by its address (Wi-Fi/LAN,
 * Tailscale, an https address) shows under the grant of its own origin, made while the app runs (`sessionPageOrigin`,
 * NSH 0.7's `allowOrigin` with `webview.runtimeOrigin`).
 */
export function sessionPageShowsInApp(url: string, os: string): boolean {
  if (os === "web") return true;
  return sessionPageOrigin(url) !== null;
}

/** The page's origin (`http(s)://host[:port]`), the one more origin its web view is granted; null for anything else. */
export function sessionPageOrigin(url: string): string | null {
  try {
    const page = new URL(url);
    return page.protocol === "http:" || page.protocol === "https:" ? page.origin : null;
  } catch {
    return null;
  }
}

/** Asks the computer for the page's grant (device-signed) and returns its URL. */
export async function grantSessionPage(server: StoredServer, request: SessionFileBinaryRequest): Promise<SessionPage> {
  const response = await signedFetch(server, "POST", "/session-page-grant", "mewla-session-file", {
    body: { worker_id: request.workerId, process_id: request.processId, started_at: request.startedAt, path: request.path, generation: request.generation },
  });
  if (!response.ok) {
    const error = new Error(sessionPageGrantError(response.status)) as Error & { code?: string };
    if (response.status === 409) error.code = "session_file_changed";
    throw error;
  }
  const answer = (await response.json().catch(() => null)) as { version?: unknown; path?: unknown; expires_at_ms?: unknown } | null;
  const url = answer?.version === 1 && typeof answer.path === "string" ? sessionPageUrl(serverURLNow(server), answer.path) : null;
  const expiresAtMS = typeof answer?.expires_at_ms === "number" ? answer.expires_at_ms : 0;
  if (!url || expiresAtMS <= Date.now()) throw new Error("The computer returned an invalid page link. Refresh and try again.");
  return { url, expiresAtMS };
}

function sessionPageGrantError(status: number): string {
  switch (status) {
    case 404:
      return "This computer runs an older Mewla that cannot show HTML pages. Update it, then refresh.";
    case 409:
      return "The Session or file changed before the page could open. Refresh and try again.";
    case 413:
      return "This page is larger than Mewla shows (64 MB).";
    case 415:
      return "This file is not an HTML page.";
    default:
      return `Could not open the page (HTTP ${status}). Refresh and try again.`;
  }
}

/**
 * What the page's message asks the app to open in the system browser: a web address only. The page
 * is untrusted, so anything else (the app's own `mewla:` links, `file:`, `javascript:`) is ignored.
 */
export function sessionPageLinkToOpen(data: string): string | null {
  let value: unknown;
  try {
    value = JSON.parse(data);
  } catch {
    return null;
  }
  const open = value && typeof value === "object" ? (value as { open?: unknown }).open : undefined;
  if (typeof open !== "string" || open.length > 4096) return null;
  try {
    const url = new URL(open);
    return (url.protocol === "https:" || url.protocol === "http:") && !url.username && !url.password ? url.toString() : null;
  } catch {
    return null;
  }
}

/**
 * Run in the page by the phones' and the Mac's web view: a link or `window.open` to anywhere but the
 * page's own grant asks the app to open it in the system browser, since the web view navigates only
 * within the app. (A browser's frame opens such links in a tab of its own.)
 */
export const SESSION_PAGE_LINKS_SCRIPT = `(function () {
  if (window.__mewlaPageLinks || !window.nsh || !window.nsh.postMessage) return;
  window.__mewlaPageLinks = true;
  var grant = location.pathname.match(/^\\/session-page\\/[^/]+\\//);
  var scope = location.origin + (grant ? grant[0] : "/");
  function away(href) {
    try {
      var url = new URL(href, location.href);
      if ((url.protocol === "http:" || url.protocol === "https:") && url.href.indexOf(scope) !== 0) {
        window.nsh.postMessage(JSON.stringify({ open: url.href }));
        return true;
      }
    } catch (e) {}
    return false;
  }
  document.addEventListener("click", function (event) {
    var link = event.target && event.target.closest ? event.target.closest("a[href]") : null;
    if (link && away(link.href)) event.preventDefault();
  }, true);
  var open = window.open;
  window.open = function (href) {
    if (href && away(String(href))) return null;
    return open.apply(window, arguments);
  };
})();`;
