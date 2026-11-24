import { describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { ownEntryPlan, phonesAndBrowsers, seenWords, NOT_RUNNING, thisMacSays } from "./thisMac";

const now = new Date(2026, 10, 20, 14, 5);
const at = (d: number, h: number, m = 0) => new Date(2026, 10, d, h, m).toISOString();

describe("This Mac's phones and browsers", () => {
  test("last seen first; not the other computers, not this app", () => {
    const rows = phonesAndBrowsers([
      { id: "mac-app", name: "Mewla for Mac", added_at: at(20, 9), last_seen_at: at(20, 14, 4), app_platform: "desktop" },
      { id: "pixel", name: "Pixel 9", added_at: at(1, 9), last_seen_at: at(20, 14, 3), app_platform: "android" },
      { id: "home", name: "home-server", kind: "computer", added_at: at(1, 9), last_seen_at: at(20, 14, 4) },
      { id: "chrome", name: "Chrome", added_at: at(1, 9), last_seen_at: at(12, 9), app_platform: "web" },
      { id: "iphone", name: "iPhone", added_at: at(1, 9), last_seen_at: at(19, 21), app_platform: "ios" },
    ], "mac-app", now);
    expect(rows.map((row) => [row.name, row.icon, row.seen])).toEqual([
      ["Pixel 9", "phone", "Now"],
      ["iPhone", "phone", "Yesterday"],
      ["Chrome", "browser", "Nov 12"],
    ]);
  });
  test("earlier today is the time", () => {
    expect(seenWords(at(20, 9, 30), now)).toBe("09:30");
    expect(seenWords("", now)).toBe("");
  });
});


// Owner 11-23: Mewla for Mac always has its own computer, at its own loopback address, once.
describe("This Mac's own entry", () => {
  const KEY = "ab".repeat(32);
  const LOCAL = "ws://127.0.0.1:9876/ws";
  const mac = (id: string, url: string, extra: Partial<StoredServer> = {}): StoredServer => ({ id, name: "MacBook Pro", url, daemonId: "cd".repeat(32), daemonPublicKey: KEY, ...extra });
  const manjaro: StoredServer = { id: "m", name: "manjaro", url: "wss://manjaro.example/ws", daemonId: "ef".repeat(32), daemonPublicKey: "12".repeat(32) };

  test("none (the person removed it): paired again over loopback; the other computers untouched", () => {
    expect(ownEntryPlan([manjaro], KEY, LOCAL, "m")).toEqual({ keep: null, drop: [], rewrite: false });
  });

  test("already at its loopback address: kept as it is", () => {
    const own = mac("s1", LOCAL);
    expect(ownEntryPlan([manjaro, own], KEY.toUpperCase(), LOCAL, "m")).toEqual({ keep: own, drop: [], rewrite: false });
  });

  test("an old pairing by the hosted name, or upgraded to Mewla Relay: moved to loopback in place (same id)", () => {
    const hosted = mac("s1", "wss://quiet-otter.m.mewla.ai/ws");
    expect(ownEntryPlan([manjaro, hosted], KEY, LOCAL, "m")).toEqual({ keep: hosted, drop: [], rewrite: true });
    const relay = mac("s1", "wss://quiet-otter.m.mewla.ai/ws", { transportKind: "link", transportPin: "44".repeat(32), linkRouteId: "33".repeat(16) });
    expect(ownEntryPlan([relay], KEY, LOCAL, null)).toMatchObject({ keep: relay, rewrite: true });
  });

  test("two entries for this Mac: the loopback one stays, the other goes (no second MacBook Pro)", () => {
    const hosted = mac("s1", "wss://quiet-otter.m.mewla.ai/ws");
    const own = mac("s2", LOCAL);
    expect(ownEntryPlan([hosted, manjaro, own], KEY, LOCAL, "s1")).toEqual({ keep: own, drop: ["s1"], rewrite: false });
  });

  test("two old ones and none at loopback: the current one is moved, the other goes", () => {
    const a = mac("s1", "wss://quiet-otter.m.mewla.ai/ws");
    const b = mac("s2", "ws://192.168.1.20:9876/ws");
    expect(ownEntryPlan([a, b], KEY, LOCAL, "s2")).toEqual({ keep: b, drop: ["s1"], rewrite: true });
  });
});

describe("thisMacSays: one rule for the pane, the row and the starting window", () => {
  const REFUSED = "macOS is not letting Mewla start";
  test("the join's own reason first, in every state", () => {
    expect(thisMacSays({ problem: REFUSED, state: { entry: "none" }, words: "" })).toBe(REFUSED);
    expect(thisMacSays({ problem: REFUSED, state: { entry: "current", kind: "offline", diagnosis: "Server unreachable" }, words: "Offline · Server unreachable" })).toBe(REFUSED);
    expect(thisMacSays({ problem: REFUSED, state: { entry: "other", check: "offline" }, words: "Offline · last seen 00:34" })).toBe(REFUSED);
  });
  test("its daemon not answering reads the same in the three states", () => {
    expect(thisMacSays({ problem: NOT_RUNNING, state: { entry: "none" }, words: "" })).toBe(NOT_RUNNING);
    expect(thisMacSays({ problem: "", state: { entry: "current", kind: "offline", diagnosis: "Server unreachable" }, words: "Offline · Server unreachable" })).toBe(NOT_RUNNING);
    expect(thisMacSays({ problem: "", state: { entry: "current", kind: "offline", diagnosis: "Daemon refused connection" }, words: "Offline" })).toBe(NOT_RUNNING);
    expect(thisMacSays({ problem: "", state: { entry: "current", kind: "offline" }, words: "Offline" })).toBe(NOT_RUNNING);
    // The 0.4.27 gate (h1): its entry beside the current computer, its daemon stopped: not "Offline · last seen".
    expect(thisMacSays({ problem: "", state: { entry: "other", check: "offline" }, words: "Offline · last seen 00:34" })).toBe(NOT_RUNNING);
    expect(thisMacSays({ problem: "", state: { entry: "other" }, words: "Offline · last seen 00:34" })).toBe(NOT_RUNNING);
    expect(thisMacSays({ problem: "", state: { entry: "other" }, words: "Offline" })).toBe(NOT_RUNNING);
  });
  test("its own check outranks the current computer's view of it", () => {
    expect(thisMacSays({ problem: "", state: { entry: "other", check: "online" }, words: "Offline · last seen 00:34" })).toBe("Online");
    expect(thisMacSays({ problem: "", state: { entry: "other", check: "online" }, words: "Online · 1 needs you" })).toBe("Online · 1 needs you");
  });
  test("no entry and no reason: starting; other causes and live states keep their words", () => {
    expect(thisMacSays({ problem: "", state: { entry: "none" }, words: "" })).toBe("Starting…");
    expect(thisMacSays({ problem: "", state: { entry: "current", kind: "offline", diagnosis: "Daemon rejected handshake" }, words: "Offline · Daemon rejected handshake" })).toBe("Offline · Daemon rejected handshake");
    expect(thisMacSays({ problem: "", state: { entry: "current", kind: "online" }, words: "Online" })).toBe("Online");
    expect(thisMacSays({ problem: "", state: { entry: "current", kind: "connecting" }, words: "Connecting…" })).toBe("Connecting…");
    expect(thisMacSays({ problem: "", state: { entry: "other", check: "unpaired" }, words: "Pair again" })).toBe("Pair again");
  });
});
