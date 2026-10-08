export type PluginIntegration = {
  id: string;
  name: string;
  available: boolean;
  unavailable_reason?: string;
  setup_url: string;
  description: string;
};
export type ConnectionTool = {
  name: string;
  description: string;
  input_schema?: Record<string, unknown>;
  allowed: boolean;
  group?: "read" | "write";
};
export type PluginAccount = {
  id: string;
  integration: string;
  name: string;
  identity: string;
  endpoint?: string;
  trusted_networks?: string[];
  auth_method?: string;
  scopes?: string[];
  enabled: boolean;
  credential_removal_pending?: boolean;
  status: "connected" | "configured" | "error" | "authorization_required" | "disconnected";
  verified_at?: string;
  tools?: ConnectionTool[];
  history: { at: string; tool: string; status: string; message?: string }[];
};
export type ConnectionRequest = {
  flow_id?: string;
  callback?: string;
  group?: "read" | "write";
  action: "connect_start" | "connect_status" | "connect_finish" | "connect_cancel" | "github_preview" | "github_import" | "permissions" | "oauth_configure" | "oauth_start" | "list" | "add" | "get" | "refresh" | "enable" | "disable" | "disconnect" | "policy";
  id?: string;
  tool?: string;
  allowed?: boolean;
  input?: { integration: string; name?: string; endpoint?: string; credential?: string; spec?: unknown; trusted_networks?: string[]; allow_writes?: boolean; web_origin?: string; oauth_client?: { client_id?: string; client_secret?: string; redirect_url: string; resource_url?: string } };
};
export type ConnectFlow = {
  id: string;
  integration: string;
  status: "waiting" | "confirm" | "connected" | "cancelled" | "failed";
  authorization_url?: string;
  user_code?: string;
  identity?: string;
  message?: string;
  expires: string;
  account_id?: string;
  /** The provider returns this browser tab to the web UI's Plugins page. */
  web_return?: boolean;
};
export type ConnectionResponse = {
  flow?: ConnectFlow;
  authorization_url?: string;
  oauth_configured?: string[];
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
