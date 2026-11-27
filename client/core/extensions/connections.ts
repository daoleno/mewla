export type PluginIntegration = {
  cli?: { version: string; managed: boolean };
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
/** AWS: who is connected, read by the server when the account is connected or refreshed. */
export type AwsAccountDetails = {
  account_id: string;
  /** The AWS account's name or alias; absent when the sign-in may read neither. */
  account_name?: string;
  account_state?: string;
  /** The signed-in identity in words: "Root user", a role's name, "IAM user NAME". */
  who: string;
  /** The role session's name. */
  session?: string;
  checked_at?: string;
};
export type PluginAccount = {
  checking?: boolean;
  profile?: string;
  profile_kind?: string;
  profile_status?: string;
  profile_sign_in_by?: string;
  /** maximum: aws login's 12 h at most; session: an Identity Center sign-in's real end; sso: a legacy token's. */
  profile_sign_in_limit?: "maximum" | "sso" | "session";
  /** An Identity Center profile: when Mewla signed its session in. */
  profile_signed_in_at?: string;
  /** How long the organization's Identity Center sign-ins last, in seconds; absent when not known yet. */
  profile_session_length_s?: number;
  project?: string;
  active?: boolean;
  id: string;
  integration: string;
  name: string;
  identity: string;
  endpoint?: string;
  trusted_networks?: string[];
  auth_method?: string;
  scopes?: string[];
  /** What the last sign-in asked for and the service left out of its grant. */
  declined_scopes?: string[];
  /** Hyperliquid: an account is one network. */
  network?: "mainnet" | "testnet";
  /** Hyperliquid: the account a trading key trades for; absent on the keyless reader. */
  address?: string;
  /** Hyperliquid: the saved API wallet's public address; absent when the account is read only. */
  signer?: string;
  /** AWS: the account's default region. */
  region?: string;
  /** AWS: who is connected. */
  aws?: AwsAccountDetails;
  enabled: boolean;
  credential_removal_pending?: boolean;
  status: "connected" | "configured" | "error" | "authorization_required" | "disconnected";
  verified_at?: string;
  /** Why the service cannot be reached right now; Mewla retries by itself. */
  unavailable?: string;
  unavailable_since?: string;
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
  /** The sign-in predates reads Mewla now offers (Slack's files and private conversations): Reconnect asks for them. */
  read_consent?: boolean;
  /** Changes are allowed, but the sign-in predates a change Mewla now offers (Slack's files): Reconnect asks for it. */
  write_upgrade?: boolean;
  allowed: number;
  tools: number;
};
export type ConnectionRequest = {
  flow_id?: string;
  callback?: string;
  group?: "read" | "write";
  confirm_profile?: string;
  profile?: string;
  /** cloud_session_length: an Identity Center sign-in's length, in seconds. */
  seconds?: number;
  action: "cloud_rename" | "cloud_delete" | "cloud_session_length" | "connect_start" | "connect_status" | "connect_finish" | "connect_cancel" | "github_preview" | "github_import" | "permissions" | "oauth_configure" | "oauth_start" | "list" | "add" | "get" | "refresh" | "enable" | "disable" | "disconnect" | "policy" | "confirm_updates" | "dismiss_updates" | "remove_key";
  id?: string;
  tool?: string;
  /** policy: several tools at once, all of them or none. confirm_updates and dismiss_updates: the updated tools shown. */
  tools?: string[];
  allowed?: boolean;
  input?: { integration: string; profile?: string; name?: string; endpoint?: string; credential?: string; network?: "mainnet" | "testnet"; account_id?: string; spec?: unknown; trusted_networks?: string[]; allow_writes?: boolean; web_origin?: string; oauth_client?: { client_id?: string; client_secret?: string; redirect_url: string; resource_url?: string }; sso_start_url?: string; sso_region?: string; aws_login?: boolean };
};
export type ConnectFlow = {
  code_required?: boolean;
  /** Adding AWS accounts needs the organization's access portal (IAM Identity Center) first, or aws login. */
  portal_required?: boolean;
  /** The CLI needs a one-time client from the person, pasted as the code (Google Workspace). */
  setup_required?: boolean;
  install_required?: boolean;
  install_manual?: boolean;
  install_failed?: boolean;
  install_url?: string;
  installing?: boolean;
  id: string;
  integration: string;
  status: "waiting" | "confirm" | "connected" | "cancelled" | "failed";
  authorization_url?: string;
  user_code?: string;
  /** The sign-in returns to the server's own address; from another device the person pastes the address the browser ended on (connect_finish with callback). */
  address_required?: boolean;
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
  if (!account.enabled) return "Off";
  // Signed in before Mewla read more of the service (Slack's files and DMs): works, but wants one Reconnect.
  if (account.status === "connected" && account.access?.read_consent) return "Needs Reconnect";
  return {
    connected: "Connected",
    configured: "Not checked yet",
    error: account.unavailable ? "Unavailable" : "Last call failed",
    authorization_required: "Needs sign-in again",
  }[account.status];
}
