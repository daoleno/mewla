import { describe, expect, test } from "bun:test";
import {
  activeModifiers,
  connectionQuality,
  formatStats,
  KEYBOARD_SENTINEL,
  keyboardEdit,
  NO_MODIFIERS,
  powerSummary,
  powerWarning,
  qualityReduced,
  slowestStage,
  spendModifiers,
  streamHeadline,
  tapModifier,
  unsupportedScreen,
} from "./screenPageModel";
import { parseScreenViewerMessage } from "./screenViewerProtocol";
import type { ScreenStatus } from "../../services/screen";
import { parseStreamStats, type ScreenStreamStats } from "../../services/screenStream";

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
    expect(powerWarning(status({ power: { source: "battery", lid_closed: true, keeping_awake: true } }))?.title).toBe("The lid is closed");
  });
  test("sleep allowed, then battery, then nothing", () => {
    expect(powerWarning(status({ keep_awake: false }))?.title).toBe("This computer may sleep");
    expect(powerWarning(status({ keep_awake: false, approved: false }))).toBeNull();
    expect(powerWarning(status({ power: { source: "battery", percent: 40, keeping_awake: true } }))?.title).toBe("On battery");
    expect(powerWarning(status({}))).toBeNull();
    expect(powerWarning(status({ power: undefined }))).toBeNull();
  });
  test("summary", () => {
    expect(powerSummary({ source: "ac", percent: 90, keeping_awake: true }, true)).toBe("Charger · 90% · Staying awake");
    expect(powerSummary({ source: "battery", percent: 40, keeping_awake: false }, false)).toBe("Battery · 40% · May sleep");
    expect(powerSummary(undefined, true)).toBeNull();
  });
});

describe("unsupported computers", () => {
  const status = (platform: string, reason?: string): ScreenStatus => ({
    supported: false, platform, reason, disabled: false, approved: false, ready: false, keep_awake: false,
  });
  test("away from a Mac, the app points to switching computers", () => {
    const linux = unsupportedScreen(status("linux", "Screen works only on Macs for now"), "manjaro", true);
    expect(linux.title).toBe("Screen works on Macs for now");
    expect(linux.detail).toContain("switch to that computer");
    expect(linux.canChooseComputer).toBe(true);
  });
  test("a browser can't switch, so it says where to go instead", () => {
    const web = unsupportedScreen(status("linux"), "manjaro", false);
    expect(web.canChooseComputer).toBe(false);
    expect(web.detail).toContain("open Mewla on your phone");
  });
  test("a Mac that can't start keeps its own reason", () => {
    const mac = unsupportedScreen(status("darwin", "Screen needs macOS 13 or later"), "MacBook", true);
    expect(mac.detail).toBe("Screen needs macOS 13 or later");
    expect(mac.canChooseComputer).toBe(false);
  });
});

describe("stats", () => {
  test("format and quality", () => {
    expect(formatStats({ rttMs: 42.4, inputMs: 120, fps: 7.25, kbps: 1500 })).toBe("42 ms network · 120 ms to screen · 7.3 fps · 1.5 Mb/s");
    expect(formatStats({ rttMs: null, inputMs: null, fps: 30, kbps: 80 })).toBe("30 fps · 80 kb/s");
    expect(connectionQuality(30)).toBe("good");
    expect(connectionQuality(150)).toBe("fair");
    expect(connectionQuality(400)).toBe("poor");
    expect(connectionQuality(null)).toBe("unknown");
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
  path: "relay", updates: 24, bytes: 400_000, dirtyPct: 60, captureSeq: 90, captureMs: 12, encodeMs: 10, encodeMaxMs: 15,
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
  test("the headline says the path first, then the slowest stage, then reduced quality", () => {
    expect(streamHeadline(readout())).toBe("Relay · network 142 ms");
    expect(streamHeadline(readout({ daemon: daemonStats({ path: "tailscale", quality: 3 }) }))).toBe("Tailscale · network 142 ms · reduced quality");
    expect(streamHeadline(readout({ daemon: daemonStats({ path: "lan" }) }))).toContain("LAN");
  });
  test("the path the phone chose is named, not the address the daemon saw", () => {
    // Through the phone's loopback tunnel the daemon may see "Local" or "Direct".
    expect(streamHeadline(readout({ viaPath: "lan", daemon: daemonStats({ path: "loopback" }) }))).toBe("LAN · network 142 ms");
    expect(streamHeadline(readout({ viaPath: "relay", daemon: daemonStats({ path: "direct" }) }))).toBe("Relay · network 142 ms");
    expect(streamHeadline(readout({ viaPath: "tailscale", daemon: null }))).toBe("Tailscale · network 142 ms");
    expect(streamHeadline(readout({ viaPath: null, daemon: daemonStats({ path: "lan" }) }))).toContain("LAN");
  });
  test("an older daemon's stream still has a headline from what the phone measured", () => {
    expect(streamHeadline(readout({ daemon: null }))).toBe("network 142 ms");
    expect(streamHeadline(readout({ daemon: null, rttMs: null, bridgeMs: null, presentMs: null }))).toBeNull();
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
