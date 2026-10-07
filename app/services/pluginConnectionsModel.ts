import type { ConnectPhase, PendingConnection } from "./pluginOnboarding";
import { accountStatus, type PluginAccount, type PluginIntegration } from "./connections";

export type ConnectionTone = "success" | "warning" | "danger" | "neutral" | "accent";

export interface ConnectedAccountEntry {
  account: PluginAccount;
  plugin: PluginIntegration;
}

export interface ServiceEntry {
  plugin: PluginIntegration;
  /** Accounts on this server that are not disconnected. */
  accountCount: number;
}

export interface PluginCatalogSections {
  connected: ConnectedAccountEntry[];
  services: ServiceEntry[];
  custom: ServiceEntry[];
}

/**
 * Splits the current server's catalog into accounts already connected,
 * reviewed built-in services and custom services. A built-in service is one
 * with a reviewed capability description; everything else is custom.
 */
export function pluginCatalogSections(
  catalog: readonly PluginIntegration[],
  accounts: readonly PluginAccount[],
  reviewed: Readonly<Record<string, unknown>>,
): PluginCatalogSections {
  const active = accounts.filter((account) => account.status !== "disconnected");
  const byId = new Map(catalog.map((plugin) => [plugin.id, plugin]));
  const count = (id: string) => active.filter((account) => account.integration === id).length;
  const connected = active
    .flatMap((account) => {
      const plugin = byId.get(account.integration);
      return plugin ? [{ account, plugin }] : [];
    })
    .sort((a, b) => a.plugin.name.localeCompare(b.plugin.name) || a.account.name.localeCompare(b.account.name));
  const entries = catalog.map((plugin) => ({ plugin, accountCount: count(plugin.id) }));
  return {
    connected,
    services: entries.filter((entry) => Boolean(reviewed[entry.plugin.id])),
    custom: entries.filter((entry) => !reviewed[entry.plugin.id]),
  };
}

export function accountTone(account: PluginAccount): ConnectionTone {
  if (account.status === "disconnected" || !account.enabled) return "neutral";
  if (account.status === "connected") return "success";
  if (account.status === "configured") return "neutral";
  if (account.status === "authorization_required") return "warning";
  return "danger";
}

export type CapabilityState = "allowed" | "off" | "unavailable";

/** Group-level access for one account, derived only from its reported tools. */
export function capabilityState(account: PluginAccount, group: "read" | "write"): CapabilityState {
  const tools = account.tools?.filter((tool) => tool.group === group) ?? [];
  if (!tools.length) return "unavailable";
  return tools.every((tool) => tool.allowed) ? "allowed" : "off";
}

export function capabilityLabel(state: CapabilityState): string {
  return state === "allowed" ? "Allowed" : state === "off" ? "Off" : "Not granted";
}

export function allowedToolCount(account: PluginAccount): { allowed: number; total: number } {
  const tools = account.tools ?? [];
  return { allowed: tools.filter((tool) => tool.allowed).length, total: tools.length };
}

export type AccountRecoveryAction = "refresh" | "reconnect" | "enable" | "disconnect";

export interface AccountRecovery {
  title: string;
  detail: string;
  action: AccountRecoveryAction;
  actionLabel: string;
  tone: "warning" | "danger" | "neutral";
}

/**
 * The single most useful recovery for an account that is not working.
 * Returns null when the account is connected and enabled.
 */
export function accountRecovery(account: PluginAccount, serviceName: string): AccountRecovery | null {
  if (account.credential_removal_pending) {
    return { title: "Credential removal pending", detail: "Calls are stopped. Mewla still needs to remove the saved credential.", action: "disconnect", actionLabel: "Retry", tone: "danger" };
  }
  if (!account.enabled) {
    return { title: "Paused", detail: "Brain and Agents can't use this account.", action: "enable", actionLabel: "Resume", tone: "neutral" };
  }
  if (account.status === "authorization_required") {
    return { title: "Reconnect required", detail: `${serviceName} no longer accepts the saved authorization.`, action: "reconnect", actionLabel: "Reconnect", tone: "warning" };
  }
  if (account.status === "error") {
    return { title: accountStatus(account), detail: "Check the service and its tools on this server.", action: "refresh", actionLabel: "Check", tone: "danger" };
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
