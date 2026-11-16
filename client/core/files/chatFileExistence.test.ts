import { describe, expect, test } from "bun:test";
import { createChatFileExistence } from "./chatFileExistence";

/** A cache whose batches go when `send()` is called, and an `ask` that records each batch and answers from `files`. */
function setup(files: string[], options: { fail?: (batch: string[]) => unknown } = {}) {
  const pending: Array<() => void> = [];
  let changes = 0;
  const cache = createChatFileExistence({
    onChange: () => changes++,
    isFinalRefusal: (error) => error === "unknown",
    schedule: (run) => pending.push(run),
  });
  const batches: string[][] = [];
  const ask = (batch: string[]) => {
    batches.push(batch);
    const failure = options.fail?.(batch);
    return failure === undefined ? Promise.resolve(batch.filter((path) => files.includes(path))) : Promise.reject(failure);
  };
  const send = async () => {
    pending.splice(0).forEach((run) => run());
    await Promise.resolve();
    await Promise.resolve();
  };
  return { cache, ask, batches, send, changes: () => changes };
}

describe("which paths Chat replies name are files (owner 11-16)", () => {
  test("rows asking in one tick share one request; the answer links them and is kept", async () => {
    const t = setup(["worklog/a/DESIGN.md", "RESULT.md"]);
    expect([...t.cache.existing("s1", ["worklog/a/DESIGN.md", "client/x.ts"], t.ask)]).toEqual([]);
    expect([...t.cache.existing("s1", ["RESULT.md", "client/x.ts"], t.ask)]).toEqual([]);
    await t.send();
    expect(t.batches).toEqual([["worklog/a/DESIGN.md", "client/x.ts", "RESULT.md"]]);
    expect(t.changes()).toBe(1);
    expect([...t.cache.existing("s1", ["worklog/a/DESIGN.md", "client/x.ts", "RESULT.md"], t.ask)]).toEqual(["worklog/a/DESIGN.md", "RESULT.md"]);
    await t.send();
    expect(t.batches).toHaveLength(1);
  });

  test("an answer with no file in it draws nothing again", async () => {
    const t = setup([]);
    t.cache.existing("s1", ["docs/a.md"], t.ask);
    await t.send();
    expect(t.changes()).toBe(0);
    t.cache.existing("s1", ["docs/a.md"], t.ask);
    await t.send();
    expect(t.batches).toHaveLength(1);
  });

  test("each run of a Session (and each server) is asked on its own", async () => {
    const t = setup(["a.md"]);
    t.cache.existing("srvA|w1|10|1", ["a.md"], t.ask);
    t.cache.existing("srvB|w1|10|1", ["a.md"], t.ask);
    await t.send();
    expect(t.batches).toEqual([["a.md"], ["a.md"]]);
  });

  test("a failed request leaves its paths unknown, asked again next time", async () => {
    let fail = true;
    const t = setup(["a.md"], { fail: () => (fail ? new Error("Server disconnected.") : undefined) });
    t.cache.existing("s1", ["a.md"], t.ask);
    await t.send();
    fail = false;
    expect([...t.cache.existing("s1", ["a.md"], t.ask)]).toEqual([]);
    await t.send();
    expect([...t.cache.existing("s1", ["a.md"], t.ask)]).toEqual(["a.md"]);
    expect(t.batches).toHaveLength(2);
  });

  test("a computer without the request is not asked again for that run", async () => {
    const t = setup(["a.md"], { fail: () => "unknown" });
    t.cache.existing("s1", ["a.md"], t.ask);
    await t.send();
    t.cache.existing("s1", ["a.md", "b.md"], t.ask);
    await t.send();
    expect(t.batches).toHaveLength(1);
  });

  test("an answer that lands after clear() is dropped", async () => {
    const t = setup(["a.md"]);
    t.cache.existing("s1", ["a.md"], t.ask);
    const pending = t.send();
    t.cache.clear();
    await pending;
    expect(t.changes()).toBe(0);
  });

  test("more paths than one batch go in several requests", async () => {
    const t = setup([]);
    t.cache.existing("s1", Array.from({ length: 70 }, (_, i) => `d/${i}.md`), t.ask);
    await t.send();
    expect(t.batches.map((batch) => batch.length)).toEqual([64, 6]);
  });
});
