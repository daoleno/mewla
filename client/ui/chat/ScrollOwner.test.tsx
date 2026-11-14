import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressWhenEnabled, typeUntil } from "../testing/fixtures";

/**
 * Owner 11-14: reading history, the chat jumped to the newest message; after a send it landed somewhere in history.
 * A thread past 240 events is the daemon's full window: each new event comes with the oldest deleted, and NSH's
 * List laid every row out again from estimated sizes (the reader's place and the send's scroll went with them).
 * Read from the chat's scroll trace (core/timeline/scrollTrace.ts): its offsets, its splices, what it shows.
 */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const session = { id: "w1", name: "longchat", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
const words = "A long line of words that wraps over several rows in a phone-sized chat. ";
const msg = (i: number) => ({ id: `m${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message", timestamp: new Date(Date.UTC(2026, 10, 11, 7, 0, i)).toISOString(), body: `Message ${i}. ${words.repeat(1 + (i % 4))}` });
/** The daemon's window: a conversation's newest 240 events. */
const full = Array.from({ length: 240 }, (_, i) => msg(i));

type App = Awaited<ReturnType<typeof open>>;
type Entry = { at: number; scope: string; kind: string } & Record<string, any>;
const trace = (app: App): Entry[] => app.host.posts().filter((p) => p.name === "app.scrollTrace").map((p) => p.data as Entry);
const since = (app: App, mark: number) => trace(app).slice(mark);
const lastOf = (entries: Entry[], kind: string) => [...entries].reverse().find((e) => e.kind === kind);

async function openChat(where: "session" | "brain") {
  const conversations = where === "session" ? { "brain-host": [], w1: full } : { "brain-host": full };
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations }) });
  if (where === "session") await app.host.send("links.open", { url: "mewla://sessions/w1" });
  await app.waitFor(`id:${where}-chat`, { timeout: 10_000 });
  await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 239.")), { timeout: 10_000 });
  await app.wait(400);
  return { app, worker: where === "session" ? "w1" : "brain-host" };
}

async function readBackUp(app: App, id: string) {
  for (let i = 0; i < 4; i += 1) await app.scroll(`id:${id}`, -1500);
  await app.wait(500);
  expect(app.shows("id:jump-latest")).toBe(true);
  return lastOf(trace(app), "viewable")!.firstKey as string;
}

/** The daemon's delta once its window is full: the new events, and as many of the oldest deleted. */
let oldest = 0;
let newest = 240;
function slide(app: App, worker: string, count = 1, extra: Record<string, unknown> = {}) {
  const upserts = Array.from({ length: count }, () => msg(newest++));
  const deletes = Array.from({ length: count }, () => `m${oldest++}`);
  return app.host.send("app.conversation", { worker, upserts, deletes, ...extra });
}

/** Moves the trace shows no command or finger behind: the list's offset changing by more than a row's rounding. */
function unbidden(entries: Entry[]) {
  let commanded = false;
  return entries.filter((e) => {
    if (e.kind === "cmd" || e.kind === "drag") commanded = true;
    return e.kind === "scroll" && !e.dragging && !commanded && Math.abs(e.jump) > 2;
  });
}

for (const where of ["session", "brain"] as const) {
  describe(`a full thread read back up (${where === "session" ? "a Session" : "Brain"})`, () => {
    test("its new events leave the rows under the finger where they are", async () => {
      oldest = 0;
      newest = 240;
      const { app, worker } = await openChat(where);
      const before = await readBackUp(app, `${where}-chat`);
      const mark = trace(app).length;
      for (let i = 0; i < 3; i += 1) {
        await slide(app, worker);
        await app.wait(250);
      }
      await app.wait(400);
      const after = since(app, mark);
      // Every change the list made was narrow: none laid the rows on screen out again.
      expect(after.filter((e) => e.kind === "splice" && e.covers).map((e) => `${e.index}+${e.removed}`)).toEqual([]);
      expect(unbidden(after).map((e) => e.jump)).toEqual([]);
      expect(lastOf(trace(app), "viewable")!.firstKey).toBe(before);
      expect(app.shows("id:jump-latest")).toBe(true);
    }, 60_000);
  });
}

describe("a full Session, read back up, its connection back with a snapshot that slid by several events", () => {
  test("the rows under the finger stay", async () => {
    oldest = 0;
    newest = 240;
    const { app, worker } = await openChat("session");
    const before = await readBackUp(app, "session-chat");
    const mark = trace(app).length;
    const subscribed = framesOf(app.host.posts(), "codex_conversation_subscribe").length;
    await app.host.send("app.daemonDrop");
    await slide(app, worker, 6);
    await app.waitUntil(() => framesOf(app.host.posts(), "codex_conversation_subscribe").length > subscribed, { timeout: 10_000 });
    await app.wait(800);
    const after = since(app, mark);
    expect(after.filter((e) => e.kind === "splice" && e.covers).map((e) => `${e.index}+${e.removed}`)).toEqual([]);
    expect(unbidden(after).map((e) => e.jump)).toEqual([]);
    expect(lastOf(trace(app), "viewable")!.firstKey).toBe(before);
  }, 60_000);
});

async function send(app: App, text: string) {
  const before = framesOf(app.host.posts(), "send_input").length;
  await typeUntil(app, "id:composer", text, "id:send");
  await pressWhenEnabled(app, "id:send");
  await app.waitUntil(() => framesOf(app.host.posts(), "send_input").length > before);
  return framesOf(app.host.posts(), "send_input").at(-1)!.request_id as string;
}

for (const from of ["the end", "far up"] as const) {
  describe(`a send from ${from} of a full Session`, () => {
    test("ends at the newest end with the sent message on screen, through its echo and the reply sliding the window", async () => {
      oldest = 0;
      newest = 240;
      const { app, worker } = await openChat("session");
      if (from === "far up") await readBackUp(app, "session-chat");
      const mark = trace(app).length;
      const id = await send(app, "One more thing");
      await app.wait(150);
      const at = new Date().toISOString();
      await app.host.send("app.conversation", { worker, upserts: [{ id, seq: 500, kind: "user_message", timestamp: at, body: "One more thing" }], deletes: [`m${oldest++}`], activity: { id: "turn-9", status: "running", started_at: at } });
      for (const [n, body] of ["Sure", "Sure, here", `Sure, here it is. ${words.repeat(3)}`].entries()) {
        await app.wait(150);
        await app.host.send("app.conversation", { worker, upserts: [{ id: "r1", seq: 501, kind: "assistant_message", timestamp: new Date().toISOString(), body }], deletes: n === 0 ? [`m${oldest++}`] : [] });
      }
      await app.wait(1000);
      const after = since(app, mark);
      // Something is on screen all along, and the list ends where the conversation does.
      expect(after.filter((e) => e.kind === "viewable" && e.first === -1).length).toBe(0);
      expect(Math.round(lastOf(after, "scroll")!.fromEnd)).toBeLessThanOrEqual(2);
      expect(app.shows("id:jump-latest")).toBe(false);
      expect(app.texts().some((t) => t.startsWith("One more thing"))).toBe(true);
      expect(app.texts().some((t) => t.startsWith("Sure, here it is."))).toBe(true);
    }, 60_000);
  });
}
