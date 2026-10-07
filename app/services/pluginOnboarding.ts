import type { ConnectionRequest, ConnectionResponse, ConnectFlow } from "./connections";

export const PLUGIN_CALLBACK = "mewla://plugins";
const PLUGIN_RETURN_PROTOCOL = "mewla:";
export type PendingConnection = { serverId: string; flow: ConnectFlow; callback?: string };
export type ConnectPhase = "idle" | "opening" | "waiting" | "verifying" | "connected" | "cancelled" | "failed";
export function pendingConnectionKey(serverId: string | null) {
  return `zen.plugin.authorization.${serverId?.replace(/[^a-zA-Z0-9.-]/g, "_") ?? "none"}`;
}
// Preserve a return for its original paired server while another server is
// current. This only stores the code; it never contacts or switches servers.
export async function retainPluginReturn(serverIds: string[], callback: string, storage: {
  getItemAsync(key: string): Promise<string | null>;
  setItemAsync(key: string, value: string): Promise<void>;
}): Promise<string | null> {
  for (const serverId of serverIds) {
    const key = pendingConnectionKey(serverId);
    const raw = await storage.getItemAsync(key);
    if (!raw) continue;
    let pending: PendingConnection;
    try { pending = JSON.parse(raw); } catch { continue; }
    if (pending.serverId !== serverId || !pending.flow || !(Date.parse(pending.flow.expires) > Date.now()) || !matchesPluginReturn(callback, pending.flow)) continue;
    await storage.setItemAsync(key, JSON.stringify({ ...pending, callback }));
    return serverId;
  }
  return null;
}
// A bare callback without state: the browser closed on the Plugins redirect.
export function isPluginCallbackUrl(url: string): boolean {
  return url === PLUGIN_CALLBACK;
}
// Any authorization return addressed to Plugins. The Plugins flow consumes it
// from Linking; while the app runs it must not also become a navigation.
export function isPluginReturnUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === PLUGIN_RETURN_PROTOCOL && parsed.host === "plugins" && parsed.searchParams.has("state");
  } catch { return false; }
}
export function matchesPluginReturn(url: string, flow: ConnectFlow): boolean {
  try {
    const parsed = new URL(url);
    const original = new URL(flow.authorization_url ?? "");
    return parsed.protocol === PLUGIN_RETURN_PROTOCOL && parsed.host === "plugins" && !parsed.username && !parsed.password && !parsed.pathname && !parsed.hash
      && parsed.searchParams.getAll("state").length === 1 && parsed.searchParams.getAll("code").length <= 1 && parsed.searchParams.getAll("error").length <= 1 && parsed.searchParams.getAll("iss").length <= 1
      && !!parsed.searchParams.get("state") && parsed.searchParams.get("state") === original.searchParams.get("state");
  } catch { return false; }
}
// The server ID is captured at start, never read from a browser return. A
// switched server cannot receive another server's authorization code.
export async function finishPluginReturn(
  pending: PendingConnection, currentServerId: string | null, callback: string,
  send: (serverId: string, request: ConnectionRequest) => Promise<ConnectionResponse>,
): Promise<ConnectionResponse> {
  if (pending.serverId !== currentServerId || !matchesPluginReturn(callback, pending.flow)) {
    throw new Error("This authorization belongs to another connection. Return to its server and try again.");
  }
  if (Date.parse(pending.flow.expires) <= Date.now()) throw new Error("This connection expired. Connect again.");
  return send(pending.serverId, { action: "connect_finish", flow_id: pending.flow.id, callback });
}
export const pluginJobs: Record<string, { read: string; write: string; example: string }> = {
  github: { read: "Read repositories, issues and pull requests", write: "Create issues and add comments", example: "Ask Brain to summarize the open issues in a repository." },
  notion: { read: "Find and read workspace pages", write: "Create and update pages and comments", example: "Ask Brain to find and summarize a page in your workspace." },
  linear: { read: "Read issues, projects and team activity", write: "Create and update issues, projects and comments", example: "Ask Brain to summarize the issues assigned to you." },
  google: { read: "Find files, read email and check calendars", write: "Send email, update files and manage events", example: "Ask Brain to summarize your upcoming calendar events." },
  slack: { read: "Find conversations and read channel messages", write: "Send and update messages", example: "Ask Brain to summarize a channel’s recent discussion." },
};
