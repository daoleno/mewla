import { describe, expect, test } from "bun:test";
import type { PluginAccount, PluginIntegration } from "./connections";
import {
  accessControl,
  accountRecovery,
  accountTone,
  connectSteps,
  isConnecting,
  pluginCatalogRows,
  reconnectsWithWrites,
} from "./pluginConnectionsModel";

const plugin = (id: string, name: string): PluginIntegration => ({ id, name, available: true, setup_url: "", description: `${name} service` });
const account = (patch: Partial<PluginAccount>): PluginAccount => ({
  id: "a1", integration: "github", name: "octo", identity: "octo", enabled: true, status: "connected", history: [], ...patch,
});
const flow = { id: "f1", integration: "github", status: "waiting" as const, expires: "2999-01-01T00:00:00Z" };

describe("Plugins list rows", () => {
  const catalog = [plugin("github", "GitHub"), plugin("slack", "Slack"), plugin("linear", "Linear"), plugin("mcp", "MCP server")];
  const reviewed = { github: {}, slack: {}, linear: {} };

  test("one row per service, carrying its active accounts; unreviewed services are custom", () => {
    const rows = pluginCatalogRows(catalog, [
      account({ id: "a2", name: "zed" }),
      account({ id: "a1", name: "amy" }),
      account({ id: "a3", name: "gone", status: "disconnected" }),
      account({ id: "a4", name: "stray", integration: "unknown" }),
    ], reviewed);
    expect(rows.services.map((row) => row.plugin.id)).toEqual(["github", "slack", "linear"]);
    expect(rows.services[0]!.accounts.map((item) => item.id)).toEqual(["a1", "a2"]);
    expect(rows.custom.map((row) => row.plugin.id)).toEqual(["mcp"]);
  });

  test("the row offers Connect, Reconnect on the account that needs it, or the worst status", () => {
    const rows = pluginCatalogRows([...catalog, { ...plugin("google", "Google"), available: false }], [
      account({ id: "l1", integration: "linear", status: "authorization_required" }),
      account({ id: "g1" }),
      account({ id: "g2", status: "error" }),
    ], { ...reviewed, google: {} });
    const state = (id: string) => rows.services.find((row) => row.plugin.id === id)!.state;
    expect(state("slack")).toEqual({ kind: "connect" });
    expect(state("google").kind).toBe("unavailable");
    expect(state("linear")).toMatchObject({ kind: "reconnect", account: { id: "l1" } });
    expect(state("github")).toMatchObject({ kind: "status", label: "Last call failed", tone: "danger" });
  });
});

describe("account status and capability boundaries", () => {
  test("tone follows status, and a paused account is never shown as healthy", () => {
    expect(accountTone(account({}))).toBe("success");
    expect(accountTone(account({ enabled: false }))).toBe("neutral");
    expect(accountTone(account({ status: "authorization_required" }))).toBe("warning");
    expect(accountTone(account({ status: "error" }))).toBe("danger");
  });

  test("access shows a switch, a sign-in that asks again, or nothing", () => {
    const access = { read: "partial" as const, write: "off" as const, allowed: 1, tools: 3 };
    expect(accessControl(account({ access }), "read")).toMatchObject({ kind: "switch", on: true });
    expect((accessControl(account({ access }), "read") as { note?: string }).note).toMatch(/Newer tools/);
    expect(accessControl(account({ access }), "write")).toEqual({ kind: "switch", on: false, note: undefined });
    expect(accessControl(account({ access: { ...access, write_consent: true } }), "write")).toEqual({ kind: "consent" });
    expect(accessControl(account({ access: { ...access, write: "none" } }), "write")).toEqual({ kind: "none" });
    expect(accessControl(account({}), "read")).toEqual({ kind: "none" });
  });

  test("reconnecting keeps changes an account allowed", () => {
    const access = { read: "allowed" as const, write: "allowed" as const, allowed: 2, tools: 2 };
    expect(reconnectsWithWrites(account({ access }))).toBe(true);
    expect(reconnectsWithWrites(account({ access: { ...access, write: "partial" } }))).toBe(true);
    expect(reconnectsWithWrites(account({ access: { ...access, write: "off" } }))).toBe(false);
    expect(reconnectsWithWrites(account({}))).toBe(false);
  });

  test("recovery names one action, credential removal first", () => {
    expect(accountRecovery(account({}), "GitHub")).toBeNull();
    expect(accountRecovery(account({ credential_removal_pending: true, enabled: false }), "GitHub")?.action).toBe("disconnect");
    expect(accountRecovery(account({ enabled: false, status: "error" }), "GitHub")?.action).toBe("enable");
    expect(accountRecovery(account({ status: "authorization_required" }), "GitHub")?.action).toBe("reconnect");
    expect(accountRecovery(account({ status: "error" }), "GitHub")?.action).toBe("refresh");
  });
});

describe("authorization progress", () => {
  test("browser step is active while waiting; a native return moves to verification", () => {
    expect(connectSteps("waiting", { serverId: "s", flow }).map((step) => step.state)).toEqual(["active", "pending"]);
    expect(connectSteps("verifying", { serverId: "s", flow, callback: "mewla://plugins?state=x" }).map((step) => step.state)).toEqual(["done", "active"]);
    expect(connectSteps("connected", null).map((step) => step.state)).toEqual(["done", "done"]);
  });

  test("a failure marks the step that failed, never a later success", () => {
    expect(connectSteps("failed", { serverId: "s", flow }).map((step) => step.state)).toEqual(["failed", "pending"]);
    expect(connectSteps("failed", { serverId: "s", flow, callback: "mewla://plugins?state=x" }).map((step) => step.state)).toEqual(["done", "failed"]);
  });

  test("device codes name the code step", () => {
    expect(connectSteps("waiting", { serverId: "s", flow: { ...flow, user_code: "ABCD-1234" } })[0]!.label).toContain("code");
    expect(isConnecting("waiting")).toBe(true);
    expect(isConnecting("failed")).toBe(false);
  });
});
