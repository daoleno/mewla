import { describe, expect, test } from "bun:test";
import type { PluginAccount, PluginIntegration } from "./connections";
import {
  accessControl,
  accountRecovery,
  accountTone,
  bulkToolNames,
  connectSteps,
  enabledRequest,
  isConnecting,
  pluginCatalogRows,
  reconnectsWithWrites,
  serviceOff,
  toolPolicyRequest,
  toolSections,
  toolsSwitch,
  toolUpdates,
  toolUpdatesRequest,
  withToolsAllowed,
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

  test("a service reads Off, muted, only when every account is off", () => {
    const rows = pluginCatalogRows(catalog, [
      account({ id: "g1", enabled: false }),
      account({ id: "g2" }),
      account({ id: "l1", integration: "linear", enabled: false, status: "error" }),
    ], reviewed);
    const row = (id: string) => rows.services.find((item) => item.plugin.id === id)!;
    expect(row("github").state).toMatchObject({ kind: "status", label: "Connected", tone: "success" });
    expect(serviceOff(row("github").accounts)).toBe(false);
    expect(row("linear").state).toEqual({ kind: "status", label: "Off", tone: "neutral" });
    expect(serviceOff(row("linear").accounts)).toBe(true);
    expect(serviceOff([])).toBe(false);
  });
});

describe("plugin on/off switch", () => {
  test("the switch asks the server to enable or disable that one account", () => {
    expect(enabledRequest(account({ id: "a9" }), false)).toEqual({ action: "disable", id: "a9" });
    expect(enabledRequest(account({ id: "a9", enabled: false }), true)).toEqual({ action: "enable", id: "a9" });
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
    // Off is the switch's state, never a fault to fix.
    expect(accountRecovery(account({ enabled: false, status: "error" }), "GitHub")).toBeNull();
    expect(accountRecovery(account({ enabled: false, status: "authorization_required" }), "GitHub")).toBeNull();
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

describe("tool switches", () => {
  const tool = (name: string, group: "read" | "write" | undefined, allowed: boolean) => ({ name, description: `${name} does a thing`, group, allowed });
  const tools = [tool("search", "read", true), tool("fetch", "read", false), tool("create", "write", false), tool("update", "write", true), tool("export", undefined, false), tool("archive", undefined, false)];
  const access = { read: "partial" as const, write: "partial" as const, allowed: 2, tools: 6 };

  test("tools group into Read and search, Make changes and unreviewed, each with its own count", () => {
    const sections = toolSections(account({ integration: "notion", tools, access }));
    expect(sections.map((section) => [section.key, section.tools.map((item) => item.name), section.allowed])).toEqual([
      ["read", ["search", "fetch"], 1],
      ["write", ["create", "update"], 1],
      ["other", ["export", "archive"], 0],
    ]);
    expect(toolSections(account({ integration: "notion", tools: tools.slice(0, 2), access })).map((section) => section.key)).toEqual(["read"]);
  });

  test("a custom service's tools are one list; no tools, no sections", () => {
    const custom = toolSections(account({ integration: "mcp", tools: [tool("a", undefined, true), tool("b", undefined, false)] }));
    expect(custom.map((section) => [section.key, section.title, section.allowed])).toEqual([["all", "Tools", 1]]);
    expect(toolSections(account({ tools: [] }))).toEqual([]);
  });

  test("a bulk switch changes only tools not already in that state", () => {
    const target = account({ tools, access });
    expect(bulkToolNames(target, tools, true)).toEqual(["fetch", "create", "export", "archive"]);
    expect(bulkToolNames(target, tools, false)).toEqual(["search", "update"]);
    expect(bulkToolNames(target, tools.slice(0, 1), true)).toEqual([]);
  });

  test("allowing all leaves changes the service must approve to its sign-in", () => {
    const target = account({ tools, access: { ...access, write_consent: true } });
    expect(bulkToolNames(target, tools, true)).toEqual(["fetch", "export", "archive"]);
    // Turning off never needs approval.
    expect(bulkToolNames(target, tools, false)).toEqual(["search", "update"]);
    expect(toolSections(target).find((section) => section.key === "write")?.consent).toBe(true);
  });

  test("a master switch allows the rest when partly on, and turns all off when on", () => {
    const partial = account({ tools, access });
    expect(toolsSwitch(partial, tools)).toEqual({ on: false, tap: "set", names: ["fetch", "create", "export", "archive"] });
    const full = withToolsAllowed(partial, tools.map((item) => item.name), true);
    expect(toolsSwitch(full, full.tools!)).toEqual({ on: true, tap: "set", names: tools.map((item) => item.name) });
    const none = withToolsAllowed(partial, tools.map((item) => item.name), false);
    expect(toolsSwitch(none, none.tools!)).toEqual({ on: false, tap: "set", names: tools.map((item) => item.name) });
    expect(toolsSwitch(partial, [])).toEqual({ on: false, tap: "none" });
  });

  test("changes awaiting approval: All tools reads on without them, Make changes asks the service", () => {
    const pending = { ...access, write_consent: true };
    // Everything but the changes is allowed; update was allowed before.
    const target = withToolsAllowed(account({ tools, access: pending }), ["fetch", "export", "archive"], true);
    expect(toolsSwitch(target, target.tools!)).toEqual({ on: true, tap: "set", names: ["search", "fetch", "update", "export", "archive"] });
    const writes = toolSections(target).find((section) => section.key === "write")!;
    expect(toolsSwitch(target, writes.tools, writes.consent)).toEqual({ on: false, tap: "consent" });
    const approved = withToolsAllowed(target, ["create"], true);
    expect(toolsSwitch(approved, approved.tools!.filter((item) => item.group === "write"), true)).toEqual({ on: true, tap: "set", names: ["create", "update"] });
    // Nothing it may allow and nothing on: the switch has no tap.
    const only = account({ tools: [tool("create", "write", false)], access: pending });
    expect(toolsSwitch(only, only.tools!)).toEqual({ on: false, tap: "none" });
  });

  test("one tool or many is one request, and the page shows it at once", () => {
    const target = account({ tools, access });
    expect(toolPolicyRequest(target, ["fetch"], true)).toEqual({ action: "policy", id: "a1", tool: "fetch", allowed: true });
    expect(toolPolicyRequest(target, ["fetch", "export"], false)).toEqual({ action: "policy", id: "a1", tools: ["fetch", "export"], allowed: false });
    const next = withToolsAllowed(target, ["fetch", "export"], true);
    expect(next.tools?.filter((item) => item.allowed).map((item) => item.name)).toEqual(["search", "fetch", "update", "export"]);
    expect(target.tools?.[1].allowed).toBe(false);
  });
});

describe("tool updates from a service", () => {
  test("one notice names what changed and confirms exactly those tools", () => {
    const target = account({ integration: "onlora", updates: ["edit_image", "generate_image"] });
    const notice = toolUpdates(target, "Onlora");
    expect(notice?.title).toBe("Onlora updated 2 tools");
    expect(notice?.detail).toContain("edit_image, generate_image");
    expect(notice?.detail).not.toMatch(/authori/i);
    expect(toolUpdatesRequest(target, notice!.names, true)).toEqual({ action: "confirm_updates", id: "a1", tools: ["edit_image", "generate_image"] });
    expect(toolUpdatesRequest(target, ["edit_image"], false)).toEqual({ action: "dismiss_updates", id: "a1", tools: ["edit_image"] });
  });
  test("a single tool and long lists read naturally", () => {
    expect(toolUpdates(account({ updates: ["a"] }), "Linear")?.title).toBe("Linear updated 1 tool");
    expect(toolUpdates(account({ updates: ["a", "b", "c", "d", "e", "f"] }), "Linear")?.detail).toContain("a, b, c, d and 2 more");
  });
  test("nothing to confirm, or an account that is off, asks nothing", () => {
    expect(toolUpdates(account({}), "Onlora")).toBeNull();
    expect(toolUpdates(account({ updates: ["a"], enabled: false }), "Onlora")).toBeNull();
    expect(toolUpdates(account({ updates: ["a"], status: "disconnected" }), "Onlora")).toBeNull();
  });
});
