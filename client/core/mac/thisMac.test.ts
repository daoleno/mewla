import { describe, expect, test } from "bun:test";
import { httpOrigin, phonesAndBrowsers, seenWords } from "./thisMac";

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
