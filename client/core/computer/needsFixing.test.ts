import { describe, expect, test } from "bun:test";
import type { AgentUseStatus } from "../settings/computer";
import type { ConnectionIssue } from "../settings/connectionIssue";
import { catNeedsYouOf, fixButtonKind, needsFixing, optionalUpdate, type NeedsFixingInput } from "./needsFixing";

const base: NeedsFixingInput = {
  connection: "connected",
  problem: "",
  issue: null,
  check: undefined,
  relay: false,
  hostProblem: undefined,
  agentUse: null,
  update: null,
  minVersion: "0.3.0",
};
const issue = (code: ConnectionIssue["code"], title = "Some title"): ConnectionIssue => ({ code, title, detail: "", hint: "" });
const agentUse = (browser: Partial<AgentUseStatus["browser"]>, computer?: AgentUseStatus["computer"]): AgentUseStatus => ({
  enabled: true,
  browser: { state: "ready", summary: "", ...browser },
  ...(computer ? { computer } : {}),
});

describe("needs fixing", () => {
  test("a connected, healthy computer needs nothing", () => {
    expect(needsFixing(base)).toEqual([]);
  });

  test("a computer that forgot the phone says Device not paired and pairs; it blocks", () => {
    const rows = needsFixing({ ...base, connection: "waiting", problem: "401", issue: issue("device_not_paired") });
    expect(rows).toEqual([{ key: "pair", title: "Device not paired", button: "Pair", action: { kind: "pair" }, blocking: true }]);
    expect(needsFixing({ ...base, connection: "waiting", check: { state: "unpaired" } })[0]!.title).toBe("Device not paired");
  });

  test("an unreachable relay computer names the relay, not the socket; Try again", () => {
    const rows = needsFixing({ ...base, connection: "waiting", problem: "socket closed", issue: issue("network_unreachable"), relay: true });
    expect(rows.map((r) => [r.title, r.button])).toEqual([["Relay unreachable", "Try again"]]);
    expect(needsFixing({ ...base, connection: "waiting", problem: "x", issue: issue("network_unreachable") })[0]!.title).toBe("Computer unreachable");
    expect(needsFixing({ ...base, connection: "waiting", problem: "x", issue: issue("proxy_error", "Proxy error") })[0]!.title).toBe("Proxy error");
  });

  test("before the phone has looked, a failing connection is Can't connect (Relay unreachable through the relay)", () => {
    expect(needsFixing({ ...base, connection: "waiting", problem: "x" })[0]!.title).toBe("Can't connect");
    expect(needsFixing({ ...base, connection: "waiting", problem: "x", relay: true })[0]!.title).toBe("Relay unreachable");
  });

  test("through the relay, the computer's own dated word names it; the phone's other findings still win", () => {
    const dated = { ...base, connection: "waiting" as const, problem: "x", relay: true, relayDiagnosis: "Relay unreachable · 13:40" };
    expect(needsFixing(dated)[0]!.title).toBe("Relay unreachable · 13:40");
    expect(needsFixing({ ...dated, issue: issue("network_unreachable") })[0]!.title).toBe("Relay unreachable · 13:40");
    expect(needsFixing({ ...dated, issue: issue("proxy_error", "Proxy error") })[0]!.title).toBe("Proxy error");
    expect(needsFixing({ ...dated, issue: issue("device_not_paired") })[0]!.title).toBe("Device not paired");
    // A computer paired by its address: its relay is not how the phone reaches it.
    expect(needsFixing({ ...dated, relay: false })[0]!.title).toBe("Can't connect");
  });

  test("connecting shows nothing to fix; offline hides what the computer reported before", () => {
    expect(needsFixing({ ...base, connection: "connecting", agentUse: agentUse({ state: "failed" }) })).toEqual([]);
    const rows = needsFixing({ ...base, connection: "waiting", problem: "x", agentUse: agentUse({ state: "failed" }), update: { version: "0.1.0", supported: true } });
    expect(rows.map((r) => r.key)).toEqual(["connection"]);
  });

  test("Brain's agent not signed in says so and offers Switch", () => {
    const rows = needsFixing({ ...base, hostProblem: { code: "not_signed_in", executor: "claude" } });
    expect(rows).toEqual([{ key: "host", title: "Claude is not signed in", button: "Switch", action: { kind: "switch_executor" }, blocking: false }]);
    expect(needsFixing({ ...base, hostProblem: { code: "not_started", executor: "claude" } })).toEqual([]);
  });

  test("a missing Chrome extension and a screen permission each have their fix", () => {
    const rows = needsFixing({ ...base, agentUse: agentUse({ state: "no_key", fix: "install_extension" }, { state: "permission", summary: "", fix: "open_settings", permission: "Accessibility" }) });
    expect(rows.map((r) => [r.title, r.button, r.action])).toEqual([
      ["Chrome extension missing", "Fix", { kind: "agent_use", fix: "install_extension" }],
      ["Accessibility access not allowed", "Fix", { kind: "agent_use", fix: "open_settings" }],
    ]);
    // Nobody started browser setup: not a problem.
    expect(needsFixing({ ...base, agentUse: agentUse({ state: "no_extension", fix: "install_extension" }) })).toEqual([]);
    // Agents turned off: nothing about their tools.
    expect(needsFixing({ ...base, agentUse: { ...agentUse({ state: "failed" }), enabled: false } })).toEqual([]);
  });

  test("an update the app requires is red; one under way has no button; an optional one is not here", () => {
    const required = needsFixing({ ...base, update: { version: "0.2.9", supported: true } });
    expect(required).toEqual([{ key: "update", title: "Update required", button: "Update", action: { kind: "update" }, blocking: true }]);
    expect(needsFixing({ ...base, update: { version: "0.2.9", supported: true, updating: true } })).toEqual([{ key: "update", title: "Updating…", blocking: false }]);
    expect(needsFixing({ ...base, update: { version: "0.3.1", supported: true, latest: "0.3.2" } })).toEqual([]);
  });

  test("an optional update is its version, and nothing when required, under way or not self-updating", () => {
    expect(optionalUpdate({ version: "0.3.1", supported: true, latest: "0.3.2" }, "0.3.0")).toBe("0.3.2");
    expect(optionalUpdate({ version: "0.2.1", supported: true, latest: "0.3.2" }, "0.3.0")).toBeNull();
    expect(optionalUpdate({ version: "0.3.1", supported: false, latest: "0.3.2" }, "0.3.0")).toBeNull();
    expect(optionalUpdate({ version: "0.3.1", supported: true, latest: "0.3.2", updating: true }, "0.3.0")).toBeNull();
  });

  test("the button is ink only when it is the one fix", () => {
    const one = needsFixing({ ...base, hostProblem: { code: "not_signed_in", executor: "codex" } });
    expect(fixButtonKind(one)).toBe("ink");
    const two = needsFixing({ ...base, hostProblem: { code: "not_signed_in", executor: "codex" }, update: { version: "0.1.0", supported: true } });
    expect(fixButtonKind(two)).toBe("ghost");
  });
});

describe("other computers and the cat", () => {
  test("the cat's dot: this computer blocked, or another computer's row has one; an optional update or offline is no dot", () => {
    const pair = needsFixing({ ...base, connection: "waiting", issue: issue("device_not_paired") });
    expect(catNeedsYouOf({ rows: pair, others: [] })).toBe(true);
    expect(catNeedsYouOf({ rows: [], others: [{ needsYou: false }, { needsYou: true }] })).toBe(true);
    const offline = needsFixing({ ...base, connection: "waiting", problem: "x" });
    expect(catNeedsYouOf({ rows: offline, others: [{ needsYou: false }] })).toBe(false);
  });
});
