import { describe, expect, test } from "bun:test";
import type { StoredServer } from "../storedServerContract";
import { httpOrigin, ownEntryPlan, phonesAndBrowsers, seenWords, thisMacWords } from "./thisMac";

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

describe("a computer's address as an http origin", () => {
  test("its WebSocket address gives the origin its files are served at", () => {
    expect(httpOrigin("ws://127.0.0.1:19877/ws")).toBe("http://127.0.0.1:19877");
    expect(httpOrigin("wss://mac.example.ts.net/ws")).toBe("https://mac.example.ts.net");
    expect(httpOrigin("http://127.0.0.1:9876/")).toBe("http://127.0.0.1:9876");
    expect(httpOrigin("mewla://settings?p=x")).toBe("");
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

describe("thisMacWords", () => {
  test("the join's own problem first", () => {
    expect(thisMacWords({ kind: "offline", diagnosis: "Server unreachable", problem: "The port 9876 is taken.", words: "Offline · Server unreachable" })).toBe("The port 9876 is taken.");
  });
  test("nothing answers at loopback: Mewla is not running, not Offline", () => {
    expect(thisMacWords({ kind: "offline", diagnosis: "Server unreachable", problem: "", words: "Offline · Server unreachable" })).toBe("Mewla is not running");
    expect(thisMacWords({ kind: "offline", diagnosis: "Daemon refused connection", problem: "", words: "Offline · Daemon refused connection" })).toBe("Mewla is not running");
    expect(thisMacWords({ kind: "offline", problem: "", words: "Offline" })).toBe("Mewla is not running");
  });
  test("another cause keeps its words; online and connecting stay", () => {
    expect(thisMacWords({ kind: "offline", diagnosis: "Daemon rejected handshake", problem: "", words: "Offline · Daemon rejected handshake" })).toBe("Offline · Daemon rejected handshake");
    expect(thisMacWords({ kind: "online", problem: "", words: "Online" })).toBe("Online");
    expect(thisMacWords({ kind: "connecting", problem: "", words: "Connecting…" })).toBe("Connecting…");
  });
});
