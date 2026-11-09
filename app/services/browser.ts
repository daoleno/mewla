import { buildAuthorizationHeader } from "./auth";
import { resolveStoredServerURL } from "./pinnedTransport";
import type { StoredServer } from "./storage";

/**
 * What the computer says about the agent browser. The browser itself is a window on the agent's own
 * monitor of the agent screen, listed and shown with the agent's other programs (services/display);
 * the one thing asked here is the Virtual screen switch.
 */
export interface BrowserResponse {
  capability?: { available: boolean; reason?: string };
  /** Whether new Workers get a virtual-screen browser by default (the Labs switch). */
  virtual_screen?: boolean;
  /** True on a computer whose system has no virtual screen (only Linux has one). */
  virtual_screen_unsupported?: boolean;
}
/** HTTP failure with the status kept apart from any body text, so callers can explain it. */
export class BrowserRequestError extends Error {
  constructor(message: string, readonly status: number, readonly detail: string) {
    super(message);
    this.name = "BrowserRequestError";
  }
}
async function endpoint(server: StoredServer, path: string) {
  const url = new URL(await resolveStoredServerURL(server));
  url.pathname = path;
  url.search = "";
  return url;
}
export async function browserRequest(
  server: StoredServer,
  request: { action: "settings"; enabled?: boolean },
  signal?: AbortSignal,
): Promise<BrowserResponse> {
  const url = await endpoint(server, "/browser");
  url.protocol = url.protocol === "wss:" ? "https:" : url.protocol === "ws:" ? "http:" : url.protocol;
  const authorization = await buildAuthorizationHeader({ daemonId: server.daemonId, purpose: "mewla-browser" });
  if (signal?.aborted) throw new Error("Browser request cancelled");
  const response = await fetch(url.toString(), {
    method: "POST", signal,
    headers: { Authorization: authorization, "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  // Servers without Browser, proxies and auth failures answer in plain text.
  const body = await response.text();
  let result: (BrowserResponse & { error?: string }) | undefined;
  try { result = body ? JSON.parse(body) : undefined; } catch { result = undefined; }
  if (!response.ok || !result) {
    throw new BrowserRequestError(result?.error || "Browser request failed", response.status, result?.error || body.trim().slice(0, 300));
  }
  return result;
}
