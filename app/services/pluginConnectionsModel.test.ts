import { describe, expect, test } from "bun:test";
import type { PluginAccount, PluginIntegration } from "./connections";
import {
  accountRecovery,
  accountTone,
  capabilityState,
  connectSteps,
  isConnecting,
  pluginCatalogSections,
} from "./pluginConnectionsModel";

const plugin = (id: string, name: string): PluginIntegration => ({ id, name, available: true, setup_url: "", description: `${name} service` });
const account = (patch: Partial<PluginAccount>): PluginAccount => ({
  id: "a1", integration: "github", name: "octo", identity: "octo", enabled: true, status: "connected", history: [], ...patch,
});
const flow = { id: "f1", integration: "github", status: "waiting" as const, expires: "2999-01-01T00:00:00Z" };

describe("Plugins catalog sections", () => {
  const catalog = [plugin("github", "GitHub"), plugin("slack", "Slack"), plugin("mcp", "MCP server")];
  const reviewed = { github: {}, slack: {} };

  test("connected lists active accounts once, with their service, and skips disconnected ones", () => {
    const sections = pluginCatalogSections(catalog, [
      account({ id: "a2", name: "zed" }),
      account({ id: "a1", name: "amy" }),
      account({ id: "a3", name: "gone", status: "disconnected" }),
      account({ id: "a4", name: "stray", integration: "unknown" }),
    ], reviewed);
    expect(sections.connected.map((entry) => entry.account.id)).toEqual(["a1", "a2"]);
    expect(sections.connected[0]!.plugin.name).toBe("GitHub");
  });

  test("reviewed services stay listed with their account count; others are custom", () => {
    const sections = pluginCatalogSections(catalog, [account({}), account({ id: "a2", status: "disconnected" })], reviewed);
    expect(sections.services.map((entry) => [entry.plugin.id, entry.accountCount])).toEqual([["github", 1], ["slack", 0]]);
    expect(sections.custom.map((entry) => entry.plugin.id)).toEqual(["mcp"]);
  });
});

describe("account status and capability boundaries", () => {
  test("tone follows status, and a paused account is never shown as healthy", () => {
    expect(accountTone(account({}))).toBe("success");
    expect(accountTone(account({ enabled: false }))).toBe("neutral");
    expect(accountTone(account({ status: "authorization_required" }))).toBe("warning");
    expect(accountTone(account({ status: "error" }))).toBe("danger");
  });

  test("a capability is allowed only when every tool in its group is allowed", () => {
    const tools = [
      { name: "read_a", description: "", allowed: true, group: "read" as const },
      { name: "read_b", description: "", allowed: false, group: "read" as const },
      { name: "write_a", description: "", allowed: true, group: "write" as const },
    ];
    expect(capabilityState(account({ tools }), "read")).toBe("off");
    expect(capabilityState(account({ tools }), "write")).toBe("allowed");
    expect(capabilityState(account({ tools: tools.slice(0, 1) }), "write")).toBe("unavailable");
    expect(capabilityState(account({ tools: undefined }), "read")).toBe("unavailable");
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
