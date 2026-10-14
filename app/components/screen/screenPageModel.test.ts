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
  spendModifiers,
  tapModifier,
  unsupportedScreen,
} from "./screenPageModel";
import { parseScreenViewerMessage } from "./screenViewerProtocol";
import type { ScreenStatus } from "../../services/screen";

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
