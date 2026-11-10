import { describe, expect, test } from "bun:test";
import {
  activeModifiers,
  connectionProblem,
  diagnosticRows,
  indicatorTone,
  KEYBOARD_SENTINEL,
  keyboardEdit,
  NO_MODIFIERS,
  nextSlowStreak,
  powerSummary,
  powerWarning,
  floatedScreenAgents,
  qualityReduced,
  screenAgents,
  screenEntry,
  slowestStage,
  spendModifiers,
  slowLasts,
  tapModifier,
  unsupportedScreen,
} from "./screenPageModel";
import { parseScreenViewerMessage } from "./screenViewerProtocol";
import type { ScreenStatus } from "./screen";
import { parseStreamStats, type ScreenStreamStats } from "./screenStream";

describe("soft keyboard diffing", () => {
  test("typed text after the sentinel is sent as text", () => {
    expect(keyboardEdit(`${KEYBOARD_SENTINEL}hi`)).toEqual({ kind: "text", text: "hi" });
  });
  test("deleting into the sentinel is backspace", () => {
    expect(keyboardEdit(KEYBOARD_SENTINEL.slice(1))).toEqual({ kind: "backspace", count: 1 });
    expect(keyboardEdit("")).toEqual({ kind: "backspace", count: 2 });
  });
  test("an unchanged field sends nothing", () => {
    expect(keyboardEdit(KEYBOARD_SENTINEL)).toEqual({ kind: "none" });
  });
  test("text typed inside the sentinel keeps only the user's characters", () => {
    expect(keyboardEdit(` x `)).toEqual({ kind: "text", text: "x" });
  });
});

describe("sticky modifiers", () => {
  test("tap arms for one key, second tap locks, third releases", () => {
    let state = tapModifier(NO_MODIFIERS, "Command");
    expect(activeModifiers(state)).toEqual(["Command"]);
    expect(activeModifiers(spendModifiers(state))).toEqual([]);
    state = tapModifier(state, "Command");
    expect(state).toEqual({ armed: [], locked: ["Command"] });
    expect(activeModifiers(spendModifiers(state))).toEqual(["Command"]);
    expect(tapModifier(state, "Command")).toEqual(NO_MODIFIERS);
  });
  test("armed and locked modifiers combine", () => {
    const state = tapModifier(tapModifier(tapModifier(NO_MODIFIERS, "Shift"), "Shift"), "Command");
    expect(activeModifiers(state).sort()).toEqual(["Command", "Shift"]);
    expect(spendModifiers(state)).toEqual({ armed: [], locked: ["Shift"] });
  });
});

describe("power", () => {
  const status = (patch: Partial<ScreenStatus>): ScreenStatus => ({
    supported: true, platform: "darwin", disabled: false, approved: true, ready: true, keep_awake: true,
    power: { source: "ac", percent: 90, keeping_awake: true }, ...patch,
  });
  test("a closed lid outranks every other warning", () => {
    expect(powerWarning(status({ power: { source: "battery", lid_closed: true, keeping_awake: true } }))).toEqual({ label: "Lid closed", action: null });
  });
  test("sleep allowed, then battery, then nothing", () => {
    expect(powerWarning(status({ keep_awake: false }))).toEqual({ label: "Keep awake", action: "keep-awake" });
    expect(powerWarning(status({ keep_awake: false, approved: false }))).toBeNull();
    expect(powerWarning(status({ power: { source: "battery", percent: 40, keeping_awake: true } }))).toEqual({ label: "On battery", action: null });
    expect(powerWarning(status({}))).toBeNull();
    expect(powerWarning(status({ power: undefined }))).toBeNull();
  });
  test("summary", () => {
    expect(powerSummary({ source: "ac", percent: 90, keeping_awake: true }, true)).toBe("Charger · 90% · Staying awake");
    expect(powerSummary({ source: "battery", percent: 40, keeping_awake: false }, false)).toBe("Battery · 40% · May sleep");
    expect(powerSummary({ source: "ac", percent: 90, keeping_awake: true, display_kept_for_agents: true }, true)).toBe("Charger · 90% · Staying awake · Display kept on for agents");
    expect(powerSummary(undefined, true)).toBeNull();
  });
});

describe("unsupported computers", () => {
  const status = (platform: string, reason?: string): ScreenStatus => ({
    supported: false, platform, reason, disabled: false, approved: false, ready: false, keep_awake: false,
  });
  test("on another system, the app points to switching computers", () => {
    const other = unsupportedScreen(status("freebsd", "Screen isn't available on this computer yet"), true);
    expect(other.title).toBe("Screen needs a Mac, Windows or Linux computer");
    expect(other.detail).toBeNull();
    expect(other.canChooseComputer).toBe(true);
  });
  test("a browser can't switch, so it says where to go instead", () => {
    const web = unsupportedScreen(status("freebsd"), false);
    expect(web.canChooseComputer).toBe(false);
    expect(web.detail).toContain("Open Mewla from a Mac");
  });
  test("Windows and Linux keep their own reason", () => {
    const windows = unsupportedScreen(status("windows", "Mewla must run in the signed-in session"), true);
    expect(windows.title).toBe("Screen can't start on this computer");
    expect(windows.detail).toBe("Mewla must run in the signed-in session");
    expect(windows.canChooseComputer).toBe(false);
    const linux = unsupportedScreen(status("linux", "Agents use a display of their own on Linux, which needs TigerVNC: Xvnc is not installed"), true);
    expect(linux.detail).toContain("TigerVNC");
  });
  test("a Mac that can't start keeps its own reason", () => {
    const mac = unsupportedScreen(status("darwin", "Screen needs macOS 13 or later"), true);
    expect(mac.detail).toBe("Screen needs macOS 13 or later");
    expect(mac.canChooseComputer).toBe(false);
    expect(unsupportedScreen(status("darwin"), true).detail).toBeNull();
  });
});

describe("viewer messages", () => {
  test("only known types and base64 payloads pass", () => {
    expect(parseScreenViewerMessage(JSON.stringify({ type: "send", data: "AQID" }))).toEqual({ type: "send", data: "AQID" });
    expect(parseScreenViewerMessage(JSON.stringify({ type: "send", data: "<script>" }))).toBeNull();
    expect(parseScreenViewerMessage(JSON.stringify({ type: "navigate", url: "https://x" }))).toBeNull();
    expect(parseScreenViewerMessage("not json")).toBeNull();
  });
});

const daemonStats = (over: Partial<ScreenStreamStats> = {}): ScreenStreamStats => ({
  path: "relay", updates: 24, bytes: 400_000, dirtyPct: 60, video: 0, captureSeq: 90, captureMs: 12, encodeMs: 10, encodeMaxMs: 15,
  ackMs: 180, ackMaxMs: 300, quality: 6, inFlight: 2, ...over,
});
const readout = (over = {}) => ({ rttMs: 142, inputMs: 210, fps: 18, kbps: 2100, bridgeMs: 12, presentMs: 30, daemon: daemonStats(), ...over });

describe("stream stages", () => {
  test("the slowest of the disjoint stages is named", () => {
    expect(slowestStage(readout())).toEqual({ stage: "network", label: "network", ms: 142 });
    expect(slowestStage(readout({ rttMs: 20, presentMs: 95 }))?.stage).toBe("draw");
    expect(slowestStage(readout({ rttMs: 20, daemon: daemonStats({ encodeMs: 64 }) }))?.stage).toBe("encode");
    expect(slowestStage(readout({ rttMs: 8, bridgeMs: 70, presentMs: 5 }))?.stage).toBe("bridge");
  });
  test("an unmeasured stage never wins, and nothing measured names nothing", () => {
    expect(slowestStage(readout({ rttMs: null, bridgeMs: undefined, presentMs: null, daemon: null }))).toBeNull();
    expect(slowestStage(readout({ rttMs: null, bridgeMs: null, presentMs: null, daemon: daemonStats({ updates: 0, encodeMs: 50 }) }))).toBeNull();
    expect(slowestStage(readout({ rttMs: 30, daemon: null, bridgeMs: null, presentMs: null }))?.stage).toBe("network");
  });
  test("a healthy stream has nothing to say, whatever the route", () => {
    expect(connectionProblem(readout())).toBeNull();
    expect(connectionProblem(readout({ viaPath: "relay", directHint: "announced" }))).toBeNull();
    expect(connectionProblem(readout({ rttMs: 199 }))).toBeNull();
    expect(connectionProblem(readout({ rttMs: null, daemon: null }))).toBeNull();
  });
  test("a slow network says so, with the way out only when the phone has one", () => {
    expect(connectionProblem(readout({ rttMs: 466 }))).toEqual({ title: "Slow connection", fix: null });
    expect(connectionProblem(readout({ rttMs: 466, viaPath: "relay", directHint: "announced" })))
      .toEqual({ title: "Slow connection", fix: "Join your computer's Wi-Fi or Tailscale" });
    expect(connectionProblem(readout({ rttMs: 466, viaPath: "relay", directHint: "none" }))?.fix).toBeNull();
    expect(connectionProblem(readout({ rttMs: 466, viaPath: "lan", directHint: "announced" }))?.fix).toBeNull();
  });
  test("reduced quality counts as slow even when the round trip is fine", () => {
    expect(connectionProblem(readout({ daemon: daemonStats({ quality: 3 }) }))?.title).toBe("Slow connection");
  });
  test("slowness must last before it shows, and a good sample ends it", () => {
    let streak = 0;
    for (let i = 0; i < 2; i++) streak = nextSlowStreak(streak, true);
    expect(slowLasts(streak)).toBe(false);
    streak = nextSlowStreak(streak, true);
    expect(slowLasts(streak)).toBe(true);
    expect(nextSlowStreak(streak, false)).toBe(0);
  });
  test("the indicator is quiet unless something needs attention", () => {
    expect(indicatorTone(false, null)).toBe("quiet");
    expect(indicatorTone(true, null)).toBe("warn");
    expect(indicatorTone(false, { label: "Keep awake", action: "keep-awake" })).toBe("warn");
    expect(indicatorTone(false, { label: "Lid closed", action: null })).toBe("warn");
    expect(indicatorTone(false, { label: "On battery", action: null })).toBe("quiet");
  });
  test("the diagnostics list each number once, in plain labels", () => {
    const rows = diagnosticRows(readout({ rttMs: 466, inputMs: 622, fps: 29, kbps: 421, viaPath: "relay", daemon: daemonStats({ quality: 3 }) }), "Charger · 100% · Staying awake");
    expect(rows).toEqual([
      { label: "Route", value: "Relay" },
      { label: "Network", value: "466 ms" },
      { label: "To screen", value: "622 ms" },
      { label: "Frame rate", value: "29 fps" },
      { label: "Data", value: "421 kb/s" },
      { label: "Quality", value: "Reduced" },
      { label: "Computer", value: "Charger · 100% · Staying awake" },
    ]);
    expect(rows.filter((r) => r.value.includes("466")).length).toBe(1);
  });
  test("diagnostics format small rates, fast links, video and a non-network bottleneck", () => {
    const rows = diagnosticRows(readout({ rttMs: null, inputMs: null, fps: 7.25, kbps: 1500, viaPath: "lan", presentMs: 95, daemon: daemonStats({ video: 20 }) }), null);
    expect(rows).toEqual([
      { label: "Route", value: "LAN · video" },
      { label: "Frame rate", value: "7.3 fps" },
      { label: "Data", value: "1.5 Mb/s" },
      { label: "Slowest", value: "draw 95 ms" },
    ]);
  });
  test("quality is reduced only below the viewer's level", () => {
    expect(qualityReduced(daemonStats({ quality: 6 }))).toBe(false);
    expect(qualityReduced(daemonStats({ quality: 2 }))).toBe(true);
    expect(qualityReduced(null)).toBe(false);
    expect(qualityReduced(daemonStats({ quality: 0 }))).toBe(false);
  });
});

describe("daemon stream stats", () => {
  test("a stats message becomes numbers and a known path", () => {
    const stats = parseStreamStats({ type: "stats", path: "relay", updates: 3, bytes: 900, dirty_pct: 40, encode_ms: 9.5, ack_ms: 120, quality: 4, in_flight: 3 });
    expect(stats).toMatchObject({ path: "relay", updates: 3, bytes: 900, dirtyPct: 40, encodeMs: 9.5, ackMs: 120, quality: 4, inFlight: 3 });
  });
  test("anything else is dropped or zeroed rather than trusted", () => {
    expect(parseStreamStats({ type: "pong" })).toBeNull();
    const odd = parseStreamStats({ type: "stats", path: "somewhere", updates: "x", bytes: Infinity, ack_ms: null });
    expect(odd).toMatchObject({ path: null, updates: 0, bytes: 0, ackMs: 0 });
  });
});

describe("viewer messages", () => {
  test("a viewer stats message with the page's stages parses", () => {
    const raw = JSON.stringify({ type: "stats", fps: 20, kbps: 900, inputMs: 120, bridgeMs: 9, presentMs: 22, width: 1512, height: 982 });
    expect(parseScreenViewerMessage(raw)).toMatchObject({ type: "stats", bridgeMs: 9, presentMs: 22 });
  });
});

describe("a computer's own screen looked at from that computer", () => {
  const agent = { worker: "%1", name: "Spike", since: "2026-11-09T00:00:00Z" };
  const base: ScreenStatus = { supported: true, platform: "darwin", disabled: false, approved: true, ready: true, keep_awake: false };

  test("a local viewer never opens a live view, with or without an agent, whatever it asked for", () => {
    for (const how of ["auto", "watch", "takeOver"] as const) {
      expect(screenEntry({ ...base, local: true }, how)).toBe("local");
      expect(screenEntry({ ...base, local: true, agent, agents: [agent] }, how)).toBe("local");
    }
  });
  test("a viewer elsewhere still waits on an agent, or opens the live view", () => {
    expect(screenEntry({ ...base, agent, agents: [agent] })).toBe("agent");
    expect(screenEntry({ ...base, agent, agents: [agent] }, "watch")).toBe("live");
    expect(screenEntry({ ...base, agent, agents: [agent] }, "takeOver")).toBe("live");
    expect(screenEntry(base)).toBe("live");
    expect(screenEntry({ ...base, local: false })).toBe("live");
  });
  test("setup comes before anything, even locally", () => {
    expect(screenEntry({ ...base, ready: false, local: true })).toBe("setup");
  });
  test("the floating window shows the Mac's agents only to a viewer elsewhere", () => {
    expect(floatedScreenAgents({ ...base, agent, agents: [agent] })).toEqual([agent]);
    expect(floatedScreenAgents({ ...base, agent })).toEqual([agent]);
    expect(floatedScreenAgents({ ...base, local: true, agent, agents: [agent] })).toEqual([]);
    expect(floatedScreenAgents({ ...base, ready: false, agent })).toEqual([]);
  });
  test("agents are the list, else the single one", () => {
    expect(screenAgents(null)).toEqual([]);
    expect(screenAgents({ agent })).toEqual([agent]);
  });
});
