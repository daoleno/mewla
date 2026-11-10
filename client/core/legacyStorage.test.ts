import { expect, test } from "bun:test";
import { copyLegacyStorage, type LegacyStorageSource } from "./legacyStorage";
import { SERVER_KEYS } from "./servers";
import { memoryStore } from "./testing/memoryPlatform";

function source(entries: Record<string, string> | null) {
  let done = false;
  const legacy: LegacyStorageSource = {
    entries: async () => (done ? null : entries),
    copied: async () => void (done = true),
  };
  return { legacy, done: () => done };
}

test("the Expo app's computers, drafts and preferences come over once", async () => {
  const store = memoryStore();
  const { legacy, done } = source({
    [SERVER_KEYS.servers]: '[{"id":"s1"}]',
    [SERVER_KEYS.currentServer]: "s1",
    "mewla.draft.v1:s1:chat": '{"t":1,"v":"half a thought"}',
    "mewla:theme_preference": "dark",
  });
  expect(await copyLegacyStorage(legacy, store)).toHaveLength(4);
  expect(await store.getItem(SERVER_KEYS.currentServer)).toBe("s1");
  expect(await store.getItem("mewla.draft.v1:s1:chat")).toContain("half a thought");
  expect(done()).toBe(true);
  await store.setItem("mewla:theme_preference", "light");
  expect(await copyLegacyStorage(legacy, store)).toEqual([]);
  expect(await store.getItem("mewla:theme_preference")).toBe("light");
});

test("what the new app already wrote wins, and the computers come first when space runs out", async () => {
  const store = memoryStore();
  await store.setItem("mewla:theme_preference", "light");
  const { legacy } = source({
    "mewla:agent_aliases": "x".repeat(80),
    [SERVER_KEYS.servers]: '[{"id":"s1"}]',
    "mewla:theme_preference": "dark",
  });
  const copied = await copyLegacyStorage(legacy, store, 60);
  expect(copied).toEqual([SERVER_KEYS.servers]);
  expect(await store.getItem("mewla:theme_preference")).toBe("light");
});

test("a phone that never had the Expo app copies nothing", async () => {
  const { legacy } = source(null);
  expect(await copyLegacyStorage(legacy, memoryStore())).toEqual([]);
});
