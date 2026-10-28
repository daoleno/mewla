import { describe, expect, test } from "bun:test";
import { createDraftStore, DRAFT_MAX_AGE_MS, type DraftStorage } from "./draftStore";

function memory(): DraftStorage & { data: Map<string, string> } {
  const data = new Map<string, string>();
  return {
    data,
    getItem: async (key) => data.get(key) ?? null,
    setItem: async (key, value) => void data.set(key, value),
    removeItem: async (key) => void data.delete(key),
  };
}
const tick = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe("draft store", () => {
  test("a draft is written once typing pauses and comes back", async () => {
    const storage = memory();
    const store = createDraftStore(storage, { delayMs: 5 });
    store.save("a", "hel");
    store.save("a", "hello");
    expect(storage.data.size).toBe(0);
    await tick(20);
    expect(storage.data.size).toBe(1);
    expect(await store.load("a")).toBe("hello");
    expect(await store.load("b")).toBe("");
  });

  test("an empty draft, what a send leaves, removes it at once", async () => {
    const storage = memory();
    const store = createDraftStore(storage, { delayMs: 5 });
    store.save("a", "hello");
    await tick(20);
    store.save("a", "");
    await tick(1);
    expect(storage.data.size).toBe(0);
  });

  test("a send right after typing leaves nothing behind", async () => {
    const storage = memory();
    const store = createDraftStore(storage, { delayMs: 5 });
    store.save("a", "hello");
    store.save("a", "");
    await tick(20);
    expect(storage.data.size).toBe(0);
  });

  test("an old draft is dropped", async () => {
    const storage = memory();
    let clock = 1_000;
    const store = createDraftStore(storage, { delayMs: 1, now: () => clock });
    store.save("a", "hello");
    await tick(10);
    clock += DRAFT_MAX_AGE_MS + 1;
    expect(await store.load("a")).toBe("");
    expect(storage.data.size).toBe(0);
  });

  test("broken storage never throws", async () => {
    const store = createDraftStore({
      getItem: async () => { throw new Error("blocked"); },
      setItem: async () => { throw new Error("full"); },
      removeItem: async () => { throw new Error("blocked"); },
    }, { delayMs: 1 });
    store.save("a", "x");
    await tick(10);
    expect(await store.load("a")).toBe("");
  });
});
