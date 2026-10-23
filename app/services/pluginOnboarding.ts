import type { ConnectionRequest, ConnectionResponse, ConnectFlow } from "./connections";

export const PLUGIN_CALLBACK = "mewla://plugins";
const PLUGIN_RETURN_PROTOCOL = "mewla:";
export type PendingConnection = { serverId: string; flow: ConnectFlow; callback?: string };
export type ConnectPhase = "idle" | "opening" | "waiting" | "verifying" | "connected" | "cancelled" | "failed";
export function pendingConnectionKey(serverId: string | null) {
  return `mewla.plugin.authorization.${serverId?.replace(/[^a-zA-Z0-9.-]/g, "_") ?? "none"}`;
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
/** The line under "Waiting for <service>". Google's review of Mewla blocks accounts that aren't on its early-access list on Google's own page, which never comes back, so say so while waiting. */
export function waitingDetail(integration: string | undefined, serviceName: string): string {
  const base = `Finish signing in on ${serviceName}'s page. This updates when you're back.`;
  return integration === "google-calendar" || integration === "gmail"
    ? `${base} If Google says Mewla isn't verified or blocks access, this Google account isn't on Mewla's early-access list yet.`
    : base;
}
type ConnectInput = NonNullable<ConnectionRequest["input"]>;
// The web UI asks the daemon to return sign-in to its own origin; native apps
// return to PLUGIN_CALLBACK. The daemon accepts only origins it serves.
export function connectInput(input: ConnectInput, platform: string, origin?: string): ConnectInput {
  return platform === "web" && origin ? { ...input, web_origin: origin } : input;
}
// The daemon's web callback sends the browser back to Plugins with an outcome.
export function pluginReturnError(code: string | undefined): string | null {
  switch (code) {
    case undefined: case "": return null;
    case "denied": return "Access was not granted. You can connect again whenever you’re ready.";
    case "expired": return "This sign-in already finished or expired. If the service below isn’t Connected, tap its Connect to start again.";
    case "cancelled": return "This connection was cancelled. Start again to connect.";
    case "provider": return "The service didn’t complete sign-in. Start again.";
    case "issuer": return "Sign-in came back from an unexpected service. Start again.";
    case "token": return "The service didn’t accept the sign-in. Start again.";
    case "tools": return "Signed in, but the service’s tools couldn’t be loaded. Start again.";
    case "storage": return "Signed in, but this server couldn’t save the account. Start again.";
    default: return "The service couldn’t be verified. Start again.";
  }
}
export const pluginJobs: Record<string, { read: string; write: string; example: string }> = {
  github: { read: "Read repositories, issues and pull requests", write: "Create issues and add comments", example: "Ask Brain to summarize the open issues in a repository." },
  notion: { read: "Find and read workspace pages", write: "Create and update pages and comments", example: "Ask Brain to find and summarize a page in your workspace." },
  linear: { read: "Read issues, projects and team activity", write: "Create and update issues, projects and comments", example: "Ask Brain to summarize the issues assigned to you." },
  onlora: { read: "Search openly licensed images, fonts, sounds and video, and read projects, assets and files", write: "Generate and edit images, generate music, voice and video, add assets and write project files. Onlora counts generation against your quota", example: "Ask Brain to start a project and generate a character sheet." },
  cloudflare: { read: "Search Cloudflare’s API and docs, and see your account", write: "Call any Cloudflare API your sign-in allows, reads included, such as listing Workers, managing R2 buckets or editing DNS", example: "Allow changes, then ask Brain to list your Workers and R2 buckets." },
  "google-calendar": { read: "List your calendars and events and check when you are free", write: "Create, change and delete events and reply to invitations", example: "Ask Brain what is on your calendar tomorrow and when you are free on Friday." },
  gmail: { read: "Search and read your email and labels", write: "Draft and send email, mark messages read, archive and label them", example: "Ask Brain to find the latest email about your flight and summarize it." },
  slack: { read: "Find conversations and read channel messages", write: "Send and update messages", example: "Ask Brain to summarize a channel’s recent discussion." },
};
