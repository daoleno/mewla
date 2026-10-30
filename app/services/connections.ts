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
  /** The service added or changed this tool; it waits for confirmation before Brain can use it. */
  updated?: boolean;
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
  /** Hyperliquid: an account is one network. */
  network?: "mainnet" | "testnet";
  /** Hyperliquid: the account a trading key trades for; absent on the keyless reader. */
  address?: string;
  /** Hyperliquid: the saved API wallet's public address; absent when the account is read only. */
  signer?: string;
  /** AWS: the AWS CLI profile the account signs in through; absent for pasted access keys. */
  profile?: string;
  /** AWS: the profile's default region, or the region saved with pasted access keys. */
  region?: string;
  enabled: boolean;
  credential_removal_pending?: boolean;
  status: "connected" | "configured" | "error" | "authorization_required" | "disconnected";
  verified_at?: string;
  tools?: ConnectionTool[];
  history: { at: string; tool: string; status: string; message?: string }[];
  /** What Brain may do with this account, summarized by the server. */
  access?: PluginAccess;
  /** Tools the service added or changed that wait for confirmation. */
  updates?: string[];
};
/** A permission group: every reviewed tool allowed, some, none, or the account has no such tools. */
export type AccessState = "allowed" | "partial" | "off" | "none";
export type PluginAccess = {
  read: AccessState;
  write: AccessState;
  /** Allowing changes needs the service to ask again (its sign-in lacks the write scope). */
  write_consent?: boolean;
  allowed: number;
  tools: number;
};
/** A profile of the AWS CLI on the server's computer. It never carries a key. */
export type AwsProfile = {
  name: string;
  region?: string;
  /** How it signs in: `aws login`, `aws sso login`, a saved key, a role, or something else. */
  kind: "login" | "sso" | "keys" | "role" | "other";
};
export type ConnectionRequest = {
  flow_id?: string;
  callback?: string;
  /** connect_finish of an AWS sign-in: the authorization code AWS showed. */
  code?: string;
  group?: "read" | "write";
  action: "connect_start" | "connect_status" | "connect_finish" | "connect_cancel" | "github_preview" | "github_import" | "aws_profiles" | "permissions" | "oauth_configure" | "oauth_start" | "list" | "add" | "get" | "refresh" | "enable" | "disable" | "disconnect" | "policy" | "confirm_updates" | "dismiss_updates" | "remove_key";
  id?: string;
  tool?: string;
  /** policy: several tools at once, all of them or none. confirm_updates and dismiss_updates: the updated tools shown. */
  tools?: string[];
  allowed?: boolean;
  input?: { integration: string; name?: string; endpoint?: string; credential?: string; network?: "mainnet" | "testnet"; profile?: string; region?: string; spec?: unknown; trusted_networks?: string[]; allow_writes?: boolean; web_origin?: string; oauth_client?: { client_id?: string; client_secret?: string; redirect_url: string; resource_url?: string } };
};
export type ConnectFlow = {
  id: string;
  integration: string;
  status: "waiting" | "confirm" | "connected" | "cancelled" | "failed";
  authorization_url?: string;
  user_code?: string;
  /** The sign-in ends with a code the person pastes back (AWS's console sign-in). */
  code_required?: boolean;
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
  profiles?: AwsProfile[];
};
export function accountStatus(account: PluginAccount): string {
  if (account.status === "disconnected") return "Disconnected";
  if (!account.enabled) return "Off";
  return {
    connected: "Connected",
    configured: "Not checked yet",
    error: "Last call failed",
    authorization_required: "Needs sign-in again",
  }[account.status];
}
