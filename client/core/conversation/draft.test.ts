import { describe, expect, test } from "bun:test";
import { memoryStore } from "../testing/memoryPlatform";
import { chatCacheKey, Draft, DRAFT_MAX_AGE_MS, DRAFT_WRITE_DELAY_MS } from "./draft";

const wait = (ms = 5) => new Promise((resolve) => setTimeout(resolve, ms));
const KEY = "mewla.draft.v1:s1:scope:brain-thread:t1";

describe("Draft", () => {
  test("uses the Expo app's key and format", async () => {
    expect(chatCacheKey("s1", "w1", "brain-thread:t1")).toBe("s1:scope:brain-thread:t1");
    expect(chatCacheKey("s1", "w1")).toBe("s1:agent:w1");
    const storage = memoryStore({ [KEY]: JSON.stringify({ t: Date.now(), v: "half a thought" }) });
    const draft = new Draft(storage, "s1:scope:brain-thread:t1");
    expect(await draft.load()).toBe("half a thought");
    draft.save("a new thought");
    expect(JSON.parse(storage.entries.get(KEY)!).v).toBe("half a thought");
    await wait(DRAFT_WRITE_DELAY_MS + 20);
    expect(JSON.parse(storage.entries.get(KEY)!).v).toBe("a new thought");
    draft.save("");
    await wait();
    expect(storage.entries.has(KEY)).toBe(false);
  });

  test("a draft older than two weeks is dropped", async () => {
    const storage = memoryStore({ [KEY]: JSON.stringify({ t: Date.now() - DRAFT_MAX_AGE_MS - 1, v: "stale" }) });
    expect(await new Draft(storage, "s1:scope:brain-thread:t1").load()).toBe("");
    expect(storage.entries.has(KEY)).toBe(false);
  });

  test("flush keeps the last keystrokes at once", async () => {
    const storage = memoryStore();
    const draft = new Draft(storage, "s1:scope:brain-thread:t1");
    draft.save("last words");
    draft.flush();
    await wait();
    expect(JSON.parse(storage.entries.get(KEY)!).v).toBe("last words");
  });
});
