import type { ConnectPhase, PendingConnection } from "./pluginOnboarding";
import { accountStatus, type AccessState, type ConnectionRequest, type ConnectionTool, type PluginAccount, type PluginIntegration } from "./connections";

export type ConnectionTone = "success" | "warning" | "danger" | "neutral" | "accent";

/** One service on the Plugins list, with the accounts connected to it. */
export interface ServiceRow {
  plugin: PluginIntegration;
  /** Accounts on this server that are not disconnected. */
  accounts: PluginAccount[];
  state: ServiceState;
}

/**
 * What the list row offers: Connect when nothing is connected, Reconnect on
 * the account that needs it, otherwise the services' combined status.
 */
export type ServiceState =
  | { kind: "unavailable"; label: string }
  | { kind: "connect" }
  | { kind: "reconnect"; account: PluginAccount }
  | { kind: "status"; label: string; tone: ConnectionTone };

export interface PluginCatalogRows {
  services: ServiceRow[];
  custom: ServiceRow[];
}

/**
 * One row per service on the current server. A built-in service is one with
 * a reviewed capability description; everything else is a custom service.
 */
export function pluginCatalogRows(
  catalog: readonly PluginIntegration[],
  accounts: readonly PluginAccount[],
  reviewed: Readonly<Record<string, unknown>>,
): PluginCatalogRows {
  const active = accounts.filter((account) => account.status !== "disconnected");
  const rows = catalog.map((plugin) => {
    const own = active.filter((account) => account.integration === plugin.id).sort((a, b) => a.name.localeCompare(b.name));
    return { plugin, accounts: own, state: serviceState(plugin, own) };
  });
  return { services: rows.filter((row) => Boolean(reviewed[row.plugin.id])), custom: rows.filter((row) => !reviewed[row.plugin.id]) };
}

/** Services that connect with a pasted API key on their own page, not a sign-in. */
export function usesApiKey(serviceId: string): boolean {
  return serviceId === "fal" || serviceId === "image";
}

/** Services whose page holds a form: a custom service's address or an API key. */
export function connectsWithForm(serviceId: string): boolean {
  return usesApiKey(serviceId) || serviceId === "mcp" || serviceId === "openapi";
}

export function serviceState(plugin: PluginIntegration, accounts: readonly PluginAccount[]): ServiceState {
  if (!accounts.length) return plugin.available ? { kind: "connect" } : { kind: "unavailable", label: "Not yet available" };
  const expired = accounts.find((account) => account.enabled && account.status === "authorization_required");
  if (expired && plugin.available) return { kind: "reconnect", account: expired };
  // An account that is off reads Off only when every account is off.
  const on = accounts.filter((account) => account.enabled);
  const rank = (account: PluginAccount) => ({ danger: 0, warning: 1, neutral: 2, accent: 3, success: 4 })[accountTone(account)];
  const worst = [...(on.length ? on : accounts)].sort((a, b) => rank(a) - rank(b))[0]!;
  return { kind: "status", label: accountStatus(worst), tone: accountTone(worst) };
}

/** True when a service has accounts and every one of them is turned off. */
export function serviceOff(accounts: readonly PluginAccount[]): boolean {
  return accounts.length > 0 && accounts.every((account) => !account.enabled);
}

/** The plugin-level switch: off hides every tool from Brain and Workers and keeps the sign-in and tool choices. */
export function enabledRequest(account: PluginAccount, on: boolean): ConnectionRequest {
  return { action: on ? "enable" : "disable", id: account.id };
}

export function accountTone(account: PluginAccount): ConnectionTone {
  if (account.status === "disconnected" || !account.enabled) return "neutral";
  if (account.status === "connected") return "success";
  if (account.status === "configured") return "neutral";
  if (account.status === "authorization_required") return "warning";
  return "danger";
}

/** How one permission group is shown: a switch, a sign-in that asks again, or nothing. */
export type AccessControl =
  | { kind: "switch"; on: boolean; note?: string }
  | { kind: "consent" }
  | { kind: "none" };

/**
 * The control for Read and search or Make changes. A partly allowed group
 * reads as on, and points to the newer tools still waiting for review.
 */
export function accessControl(account: PluginAccount, group: "read" | "write"): AccessControl {
  const access = account.access;
  if (!access) return { kind: "none" };
  if (group === "write" && access.write_consent) return { kind: "consent" };
  const state: AccessState = access[group];
  if (state === "none") return { kind: "none" };
  return { kind: "switch", on: state !== "off", note: state === "partial" ? "Newer tools wait for your review in Tools & activity" : undefined };
}

/**
 * Reconnecting replaces the account's grant, so it asks for changes again
 * when the account allowed them; otherwise a one-tap Reconnect would drop them.
 */
export function reconnectsWithWrites(account: PluginAccount): boolean {
  return account.access?.write === "allowed" || account.access?.write === "partial";
}

export function allowedToolCount(account: PluginAccount): { allowed: number; total: number } {
  if (account.access) return { allowed: account.access.allowed, total: account.access.tools };
  const tools = account.tools ?? [];
  return { allowed: tools.filter((tool) => tool.allowed).length, total: tools.length };
}

/** One group of an account's tools on its Tools page, with its own bulk switches. */
export interface ToolSection {
  key: "read" | "write" | "other" | "all";
  title: string;
  footer?: string;
  tools: ConnectionTool[];
  allowed: number;
  /** Allowing these needs the service to ask again (its sign-in lacks write access). */
  consent: boolean;
}

/**
 * The Tools page's groups: Read and search, Make changes, then tools Mewla
 * has not reviewed. A custom service has no reviewed tools, so one list.
 */
export function toolSections(account: PluginAccount): ToolSection[] {
  const tools = account.tools ?? [];
  const section = (key: ToolSection["key"], title: string, list: ConnectionTool[], footer?: string): ToolSection => ({
    key, title, footer, tools: list, allowed: list.filter((tool) => tool.allowed).length,
    consent: key === "write" && !!account.access?.write_consent,
  });
  if (!tools.some((tool) => tool.group)) return tools.length ? [section("all", "Tools", tools)] : [];
  return [
    section("read", "Read and search", tools.filter((tool) => tool.group === "read")),
    section("write", "Make changes", tools.filter((tool) => tool.group === "write")),
    section("other", "Other tools", tools.filter((tool) => !tool.group), "Mewla hasn't reviewed these. Read what each one does before allowing it."),
  ].filter((entry) => entry.tools.length);
}

/**
 * The tools a bulk switch changes: those not already in the wanted state.
 * Allowing all leaves out changes the service still has to approve; those
 * go through its sign-in instead.
 */
export function bulkToolNames(account: PluginAccount, tools: readonly ConnectionTool[], allowed: boolean): string[] {
  const consent = allowed && !!account.access?.write_consent;
  return tools.filter((tool) => tool.allowed !== allowed && !(consent && tool.group === "write")).map((tool) => tool.name);
}

/** One policy request for one tool or many: the server applies all of them or none. */
export function toolPolicyRequest(account: PluginAccount, names: readonly string[], allowed: boolean): ConnectionRequest {
  return names.length === 1
    ? { action: "policy", id: account.id, tool: names[0], allowed }
    : { action: "policy", id: account.id, tools: [...names], allowed };
}

/** The account as the page shows it right away, before the server answers. */
export function withToolsAllowed(account: PluginAccount, names: readonly string[], allowed: boolean): PluginAccount {
  const changed = new Set(names);
  return { ...account, tools: account.tools?.map((tool) => (changed.has(tool.name) ? { ...tool, allowed } : tool)) };
}

export type AccountRecoveryAction = "refresh" | "reconnect" | "disconnect";

export interface AccountRecovery {
  title: string;
  detail: string;
  action: AccountRecoveryAction;
  actionLabel: string;
  tone: "warning" | "danger" | "neutral";
}

/**
 * The single most useful recovery for an account that is not working.
 * Returns null when the account is connected, or turned off.
 */
export function accountRecovery(account: PluginAccount, serviceName: string): AccountRecovery | null {
  if (account.credential_removal_pending) {
    return { title: "Removal didn't finish", detail: "Brain can't use this account. Mewla still has to remove its saved sign-in.", action: "disconnect", actionLabel: "Retry", tone: "danger" };
  }
  // Off is a choice, not a fault: the account's own switch turns it back on.
  if (!account.enabled) return null;
  if (account.status === "authorization_required" && account.auth_method === "api_key") {
    return { title: "Needs an API key", detail: `Enter a ${serviceName} API key. Brain can't run anything billed until then.`, action: "reconnect", actionLabel: "Enter key", tone: "warning" };
  }
  if (account.status === "authorization_required") {
    return { title: "Needs sign-in again", detail: `${serviceName} stopped accepting Mewla's sign-in.`, action: "reconnect", actionLabel: "Reconnect", tone: "warning" };
  }
  if (account.status === "error") {
    return { title: "Last call failed", detail: `Check that ${serviceName} and its tools still answer.`, action: "refresh", actionLabel: "Check again", tone: "danger" };
  }
  return null;
}

export type ConnectStepState = "done" | "active" | "pending" | "failed";

export interface ConnectStep {
  key: "authorize" | "verify";
  label: string;
  state: ConnectStepState;
}

/** Progress of one authorization attempt, from the browser to the server. */
export function connectSteps(phase: ConnectPhase, flow: PendingConnection | null): ConnectStep[] {
  const returned = Boolean(flow?.callback);
  const verifying = phase === "verifying" || returned;
  const failed = phase === "failed";
  return [
    {
      key: "authorize",
      label: flow?.flow.user_code ? "Enter the code in your browser" : "Authorize in your browser",
      state: phase === "connected" || verifying ? "done" : failed ? "failed" : phase === "opening" || phase === "waiting" ? "active" : "pending",
    },
    {
      key: "verify",
      label: "Verify on this server",
      state: phase === "connected" ? "done" : failed && returned ? "failed" : verifying ? "active" : "pending",
    },
  ];
}

export function isConnecting(phase: ConnectPhase): boolean {
  return phase === "opening" || phase === "waiting" || phase === "verifying";
}
