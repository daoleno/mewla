import { describe, expect, test } from "bun:test";
import { memoryStore } from "../testing/memoryPlatform";
import {
  getCollapsedSessionSections,
  getInterfaceRenderModes,
  getRecentWorkerOpens,
  getWorkerAliases,
  markWorkerOpened,
  setCollapsedSessionSections,
  setInterfaceRenderMode,
  setWorkerAlias,
} from "./deviceState";
import { makeSessionKey } from "./sessionKeys";

// Written literally, as the Expo app's AsyncStorage held them.
const KEY_A = '["server_lx2k_ab12cd","%12"]';
const KEY_B = '["server_lx2k_ab12cd","%3"]';

describe("Session keys", () => {
  test("are the Expo app's JSON pair", () => {
    expect(makeSessionKey("server_lx2k_ab12cd", "%12")).toBe(KEY_A);
  });
});

describe("Session nicknames", () => {
  test("read what the Expo app wrote, dropping blank and malformed entries", async () => {
    const storage = memoryStore({
      "mewla:agent_aliases": JSON.stringify({ [KEY_A]: "  Deploy  ", [KEY_B]: "   ", " ": "x", bad: 7 }),
    });
    expect(await getWorkerAliases(storage)).toEqual({ [KEY_A]: "Deploy" });
  });

  test("round-trip, and an empty nickname removes it", async () => {
    const storage = memoryStore();
    expect(await setWorkerAlias(storage, KEY_A, " Deploy ")).toEqual({ [KEY_A]: "Deploy" });
    expect(JSON.parse(storage.entries.get("mewla:agent_aliases")!)).toEqual({ [KEY_A]: "Deploy" });
    await setWorkerAlias(storage, KEY_B, "Review");
    expect(await setWorkerAlias(storage, KEY_A, "")).toEqual({ [KEY_B]: "Review" });
    expect(await getWorkerAliases(storage)).toEqual({ [KEY_B]: "Review" });
  });

  test("a value that does not parse reads as none", async () => {
    expect(await getWorkerAliases(memoryStore({ "mewla:agent_aliases": "{" }))).toEqual({});
  });
});

describe("folded sections", () => {
  test("read the Expo app's array, keeping only strings", async () => {
    const storage = memoryStore({
      "mewla:v1:collapsed_session_sections": JSON.stringify(["srv::cwd::/home/me/app", 4, "srv::fallback"]),
    });
    expect(await getCollapsedSessionSections(storage)).toEqual(["srv::cwd::/home/me/app", "srv::fallback"]);
  });

  test("round-trip, and none removes the entry", async () => {
    const storage = memoryStore();
    await setCollapsedSessionSections(storage, ["srv::project::mewla"]);
    expect(storage.entries.get("mewla:v1:collapsed_session_sections")).toBe('["srv::project::mewla"]');
    expect(await getCollapsedSessionSections(storage)).toEqual(["srv::project::mewla"]);
    await setCollapsedSessionSections(storage, []);
    expect(storage.entries.has("mewla:v1:collapsed_session_sections")).toBe(false);
    expect(await getCollapsedSessionSections(storage)).toEqual([]);
  });
});

describe("chat or terminal per Session", () => {
  test("read the Expo app's modes, dropping unknown ones", async () => {
    const storage = memoryStore({
      "mewla:codex_render_modes": JSON.stringify({ [KEY_A]: "terminal", [KEY_B]: "chat", other: "split" }),
    });
    expect(await getInterfaceRenderModes(storage)).toEqual({ [KEY_A]: "terminal", [KEY_B]: "chat" });
  });

  test("round-trip, and a blank key changes nothing", async () => {
    const storage = memoryStore();
    expect(await setInterfaceRenderMode(storage, KEY_A, "terminal")).toEqual({ [KEY_A]: "terminal" });
    expect(await setInterfaceRenderMode(storage, "  ", "chat")).toEqual({ [KEY_A]: "terminal" });
    expect(JSON.parse(storage.entries.get("mewla:codex_render_modes")!)).toEqual({ [KEY_A]: "terminal" });
  });
});

describe("recent opens", () => {
  test("read the Expo app's times, dropping non-numbers", async () => {
    const storage = memoryStore({
      "mewla:recent_agent_opens": JSON.stringify({ [KEY_A]: 1_752_960_000_000, [KEY_B]: "soon" }),
    });
    expect(await getRecentWorkerOpens(storage)).toEqual({ [KEY_A]: 1_752_960_000_000 });
  });

  test("round-trip, keeping the newest 100", async () => {
    const storage = memoryStore();
    for (let index = 0; index < 105; index += 1) {
      await markWorkerOpened(storage, makeSessionKey("srv", `%${index}`), 1000 + index);
    }
    const opens = await getRecentWorkerOpens(storage);
    expect(Object.keys(opens)).toHaveLength(100);
    expect(opens[makeSessionKey("srv", "%104")]).toBe(1104);
    expect(opens[makeSessionKey("srv", "%4")]).toBeUndefined();
    // Newest first, as the Expo app wrote it.
    expect(Object.keys(JSON.parse(storage.entries.get("mewla:recent_agent_opens")!))[0]).toBe(makeSessionKey("srv", "%104"));
  });
});
