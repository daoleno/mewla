export type PluginIntegration = {
  id: string;
  name: string;
  available: boolean;
  setup_url: string;
  description: string;
};
export type ConnectionTool = {
  name: string;
  description: string;
  input_schema?: Record<string, unknown>;
  allowed: boolean;
};
export type PluginAccount = {
  id: string;
  integration: string;
  name: string;
  identity: string;
  endpoint?: string;
  enabled: boolean;
  status: "connected" | "configured" | "error" | "authorization_required" | "disconnected";
  verified_at?: string;
  tools?: ConnectionTool[];
  history: { at: string; tool: string; status: string; message?: string }[];
};
export type ConnectionRequest = {
  action: "list" | "add" | "get" | "refresh" | "enable" | "disable" | "disconnect" | "policy";
  id?: string;
  tool?: string;
  allowed?: boolean;
  input?: { integration: string; name: string; endpoint?: string; credential?: string; spec?: unknown };
};
export type ConnectionResponse = {
  catalog?: PluginIntegration[];
  accounts?: PluginAccount[];
  account?: PluginAccount;
};
export function accountStatus(account: PluginAccount): string {
  if (account.status === "disconnected") return "Disconnected";
  if (!account.enabled) return "Disabled";
  return {
    connected: "Connected",
    configured: "Not verified",
    error: "Last call failed",
    authorization_required: "Reconnect required",
  }[account.status];
}
