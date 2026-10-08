import type { ConnectPhase, PendingConnection } from "./pluginOnboarding";
import { accountStatus, type AccessState, type PluginAccount, type PluginIntegration } from "./connections";

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

export function serviceState(plugin: PluginIntegration, accounts: readonly PluginAccount[]): ServiceState {
  if (!accounts.length) return plugin.available ? { kind: "connect" } : { kind: "unavailable", label: "Not yet available" };
  const expired = accounts.find((account) => account.enabled && account.status === "authorization_required");
  if (expired && plugin.available) return { kind: "reconnect", account: expired };
  const rank = (account: PluginAccount) => ({ danger: 0, warning: 1, neutral: 2, accent: 3, success: 4 })[accountTone(account)];
  const worst = [...accounts].sort((a, b) => rank(a) - rank(b))[0]!;
  return { kind: "status", label: accountStatus(worst), tone: accountTone(worst) };
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
 * reads as on, and says how many newer tools still wait for review; turning
 * it off and on again allows them.
 */
export function accessControl(account: PluginAccount, group: "read" | "write"): AccessControl {
  const access = account.access;
  if (!access) return { kind: "none" };
  if (group === "write" && access.write_consent) return { kind: "consent" };
  const state: AccessState = access[group];
  if (state === "none") return { kind: "none" };
  return { kind: "switch", on: state !== "off", note: state === "partial" ? "Some newer tools aren't allowed yet. Turn off and on to allow them." : undefined };
}

export function allowedToolCount(account: PluginAccount): { allowed: number; total: number } {
  if (account.access) return { allowed: account.access.allowed, total: account.access.tools };
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
    return { title: "Removal didn't finish", detail: "Brain can't use this account. Mewla still has to remove its saved sign-in.", action: "disconnect", actionLabel: "Retry", tone: "danger" };
  }
  if (!account.enabled) {
    return { title: "Off", detail: "Brain and Workers can't use this account until you turn it on.", action: "enable", actionLabel: "Turn on", tone: "neutral" };
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
