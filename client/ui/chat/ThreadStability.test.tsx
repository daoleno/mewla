import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressUntil } from "../testing/fixtures";

/**
 * Owner 11-13: Brain's whole thread flashed, sometimes several times, and a thread read back up jumped.
 * Read back up, the rows under the finger stay where they are whatever the computer sends.
 */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const long = Array.from({ length: 60 }, (_, i) => ({
  id: `m${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message",
  timestamp: new Date(Date.UTC(2026, 10, 11, 7, 0, i)).toISOString(),
  body: `Message ${i}. ` + "A long line of words that wraps over several rows in a phone-sized chat. ".repeat(4),
}));

/** The first message the list has drawn: it changes when the list moves or is made again. */
const firstDrawn = (app: { texts(): string[] }) => app.texts().find((t) => t.startsWith("Message "));

async function readBackUp() {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": long } }) });
  await app.waitFor("id:brain-chat", { timeout: 10_000 });
  await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 59.")), { timeout: 10_000 });
  await app.wait(300);
  for (let i = 0; i < 3; i += 1) await app.scroll("id:brain-chat", -1500);
  await app.wait(300);
  expect(app.shows("id:jump-latest")).toBe(true);
  return app;
}

describe("Brain's thread, read back up", () => {
  test("stays where it is when Brain's snapshot changes (a Work or a Worker changed)", async () => {
    const app = await readBackUp();
    const before = firstDrawn(app);
    for (let i = 0; i < 3; i += 1) {
      await app.host.send("app.daemonPush", { type: "brain_snapshot", brain: { ...brain, current_work: [{ work_id: `w${i}`, title: `Work ${i}`, status: "running" }] } });
      await app.wait(200);
    }
    expect(app.shows("id:jump-latest")).toBe(true);
    expect(firstDrawn(app)).toBe(before);
  }, 30_000);
});

/**
 * A row drawn anew asks again for a picture that did not load (a loaded one is kept), so the computer's
 * `codex_asset` requests count how often the picture's row was made. The thread is short: every row is drawn.
 */
const at = (i: number) => new Date(Date.UTC(2026, 10, 13, 1, 0, i)).toISOString();
const short = [
  { id: "p0", seq: 1, kind: "assistant_message", timestamp: at(0), body: "Here is the chart:\n\n![chart](out/chart.png)" },
  { id: "p1", seq: 2, kind: "user_message", timestamp: at(1), body: "Thanks. And the table?" },
  { id: "p2", seq: 3, kind: "assistant_message", timestamp: at(2), body: "Coming up." },
];
const noPicture = { codex_asset: { type: "error", message: "not a picture" } };
const builds = (app: { host: { posts(): { name: string; data: unknown }[] } }) => framesOf(app.host.posts(), "codex_asset").length;

async function everyKindOfNews(app: Awaited<ReturnType<typeof open>>, worker: string, expectBuilt: () => Promise<void>) {
  // A reply streams: the same message, longer each time, while its turn runs.
  for (const body of ["The", "The table", "The table is below."]) {
    await app.host.send("app.conversation", { worker, upserts: [{ id: "p3", seq: 4, kind: "assistant_message", timestamp: at(3), body }], activity: { id: "turn-2", status: "running", started_at: at(3) } });
    await app.wait(150);
  }
  await expectBuilt();
  // The turn ends; a new message comes.
  await app.host.send("app.conversation", { worker, upserts: [{ id: "p4", seq: 5, kind: "user_message", timestamp: at(4), body: "Great." }], activity: null });
  await app.wait(200);
  await expectBuilt();
  // The daemon sends the whole conversation again (a resync), unchanged.
  await app.host.send("app.conversation", { worker, snapshot: true });
  await app.wait(200);
  await expectBuilt();
  // The connection drops and comes back: the app subscribes again and gets the snapshot.
  const subscribed = framesOf(app.host.posts(), "codex_conversation_subscribe").length;
  await app.host.send("app.daemonDrop");
  await app.waitUntil(() => framesOf(app.host.posts(), "codex_conversation_subscribe").length > subscribed, { timeout: 10_000 });
  await app.wait(500);
  await expectBuilt();
}

describe("news changes only the rows it is about (owner 11-13: every message flashed at once)", () => {
  test("Brain's thread: a Brain snapshot, a streaming reply, a new message, a re-sent snapshot and a reconnect draw no other row again", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": short }, replies: noPicture }) });
    await app.waitFor("text:Coming up.", { timeout: 10_000 });
    await app.waitUntil(() => builds(app) >= 1);
    await app.wait(300);
    const once = builds(app);
    const unchanged = async () => expect(builds(app)).toBe(once);
    for (let i = 0; i < 3; i += 1) {
      await app.host.send("app.daemonPush", { type: "brain_snapshot", brain: { ...brain, current_work: [{ work_id: `w${i}`, title: `Work ${i}`, status: "running" }] } });
      await app.wait(200);
    }
    await unchanged();
    await everyKindOfNews(app, "brain-host", unchanged);
    expect(app.texts()).toContain("The table is below.");
    expect(app.texts()).toContain("Great.");
  }, 40_000);

  test("a Session's chat: a Session update, a streaming reply, a new message, a re-sent snapshot and a reconnect draw no other row again", async () => {
    const session = { id: "w1", name: "charts", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: short }, replies: noPicture }) });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("text:Coming up.", { timeout: 10_000 });
    await app.waitUntil(() => builds(app) >= 1);
    await app.wait(300);
    const once = builds(app);
    const unchanged = async () => expect(builds(app)).toBe(once);
    for (const status of ["running", "done"]) {
      await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status } });
      await app.wait(200);
    }
    await unchanged();
    await everyKindOfNews(app, "w1", unchanged);
  }, 40_000);
});
