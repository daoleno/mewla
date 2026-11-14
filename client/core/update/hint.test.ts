import { describe, expect, test } from "bun:test";
import type { AndroidUpdateArtifact, AppUpdateState } from "./appUpdate";
import { appUpdateMark, catUpdateHintOf, parseSeenUpdates } from "./hint";

const apk: AndroidUpdateArtifact = { version: "0.4.4", versionCode: 40400, fileName: "a.apk", url: "https://x", sha256: "c".repeat(64), size: 1 };

describe("appUpdateMark", () => {
  test("names the waiting update in every state that carries one", () => {
    const states: AppUpdateState[] = [
      { kind: "available", update: apk },
      { kind: "downloading", update: apk, progress: 0.5 },
      { kind: "needsPermission", update: apk, fileName: "a.apk" },
      { kind: "ready", update: apk, fileName: "a.apk" },
      { kind: "failed", update: apk, message: "x" },
    ];
    for (const state of states) expect(appUpdateMark(state)).toBe("0.4.4");
  });

  test("TestFlight keys on version and build: a newer build of the same version is new", () => {
    expect(appUpdateMark({ kind: "testflightAvailable", update: { version: "0.4.4", buildNumber: 7, appId: "1" } })).toBe("0.4.4+7");
  });

  test("nothing waits: no mark", () => {
    for (const kind of ["hidden", "idle", "checking", "current", "checkFailed"] as const) expect(appUpdateMark({ kind })).toBeNull();
  });
});

describe("catUpdateHintOf", () => {
  const base = { app: null, computer: null, serverId: "s1", seen: {} };

  test("no update, no dot", () => {
    expect(catUpdateHintOf(base)).toBe(false);
  });

  test("an app update not yet seen lights it; seen, it goes; the next version lights it again", () => {
    expect(catUpdateHintOf({ ...base, app: "0.4.4" })).toBe(true);
    expect(catUpdateHintOf({ ...base, app: "0.4.4", seen: { app: "0.4.4" } })).toBe(false);
    expect(catUpdateHintOf({ ...base, app: "0.4.5", seen: { app: "0.4.4" } })).toBe(true);
  });

  test("a computer's update is seen per computer", () => {
    expect(catUpdateHintOf({ ...base, computer: "0.4.4" })).toBe(true);
    expect(catUpdateHintOf({ ...base, computer: "0.4.4", seen: { computers: { s1: "0.4.4" } } })).toBe(false);
    expect(catUpdateHintOf({ ...base, computer: "0.4.4", serverId: "s2", seen: { computers: { s1: "0.4.4" } } })).toBe(true);
  });

  test("before what was seen is read, no dot (no flash at launch)", () => {
    expect(catUpdateHintOf({ ...base, app: "0.4.4", computer: "0.4.4", seen: null })).toBe(false);
  });
});

describe("parseSeenUpdates", () => {
  test("reads what it wrote and drops the rest", () => {
    expect(parseSeenUpdates(JSON.stringify({ app: "0.4.4", computers: { s1: "0.4.3", s2: 3 } }))).toEqual({ app: "0.4.4", computers: { s1: "0.4.3" } });
    expect(parseSeenUpdates(null)).toEqual({});
    expect(parseSeenUpdates("{")).toEqual({});
    expect(parseSeenUpdates("[1]")).toEqual({});
  });
});
