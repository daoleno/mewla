import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressWhenEnabled, typeUntil } from "../testing/fixtures";

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
  // The same thread under a restarted host's id too ("brain-host-2").
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": long, "brain-host-2": long } }) });
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

describe("a Session's chat read back up stays where it is (owner 11-13: history jumped)", () => {
  const session = { id: "w1", name: "longchat", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
  const words = "A long line of words that wraps over several rows in a phone-sized chat. ";
  const msg = (i: number, extra = "") => ({
    id: `m${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message",
    timestamp: new Date(Date.UTC(2026, 10, 11, 7, 0, i)).toISOString(), body: `Message ${i}. ${words.repeat(4)}${extra}`,
  });
  const history = Array.from({ length: 60 }, (_, i) => msg(i));

  async function readSessionBackUp(events = history.slice(20)) {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: events } }) });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("id:session-chat", { timeout: 10_000 });
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 59.")), { timeout: 10_000 });
    await app.wait(300);
    for (let i = 0; i < 2; i += 1) await app.scroll("id:session-chat", -1500);
    await app.wait(400);
    expect(app.shows("id:jump-latest")).toBe(true);
    return app;
  }
  const still = async (app: Awaited<ReturnType<typeof open>>, before: string | undefined, what: string) => {
    await app.wait(400);
    expect(`${what}: ${firstDrawn(app)?.slice(0, 11)}`).toBe(`${what}: ${before?.slice(0, 11)}`);
    // The jump control says there is more below (with a request open, its count).
    expect(`${what}: ${app.shows("id:jump-latest") || app.shows("id:jump-requests")}`).toBe(`${what}: true`);
  };

  test("through new messages, a streaming reply, an earlier row growing, older history, a request and a reconnect", async () => {
    const app = await readSessionBackUp();
    const before = firstDrawn(app);
    await app.host.send("app.conversation", { worker: "w1", upserts: [msg(60)] });
    await still(app, before, "a new message");
    for (const tail of ["one", "one two three", "one two three four five six seven eight nine ten. ".repeat(6)]) {
      await app.host.send("app.conversation", { worker: "w1", upserts: [{ ...msg(61), body: tail }], activity: { id: "turn-9", status: "running", started_at: new Date().toISOString() } });
      await app.wait(100);
    }
    await still(app, before, "a streaming reply");
    // A row above the screen grows (an edited or re-sent event).
    await app.host.send("app.conversation", { worker: "w1", upserts: [msg(22, words.repeat(8))], activity: null });
    await still(app, before, "an earlier row grows");
    // Older history comes in above (a resync with the whole transcript).
    await app.host.send("app.conversation", { worker: "w1", upserts: history.slice(0, 20) });
    await still(app, before, "older history");
    await app.host.send("app.conversation", { worker: "w1", snapshot: true });
    await still(app, before, "a re-sent snapshot");
    // The Session asks something: a request card arrives at the foot.
    await app.host.send("app.daemonPush", { type: "worker_session_updated", worker_session: { ...session, status: "blocked", prompt: { id: "p-1", kind: "permission", provider: "claude", title: "Run this command?", request: "ls", options: [{ number: 1, label: "Yes", selected: true }, { number: 2, label: "No" }] } } });
    await still(app, before, "a request card");
    const subscribed = framesOf(app.host.posts(), "codex_conversation_subscribe").length;
    await app.host.send("app.daemonDrop");
    await app.waitUntil(() => framesOf(app.host.posts(), "codex_conversation_subscribe").length > subscribed, { timeout: 10_000 });
    await still(app, before, "a reconnect");
  }, 60_000);
});

describe("only the person's own acts move a chat read back up", () => {
  test("a message sent earlier does not scroll the chat again when it comes back from the terminal", async () => {
    const session = { id: "w1", name: "longchat", status: "done", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: long } }) });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("id:session-chat", { timeout: 10_000 });
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 59.")), { timeout: 10_000 });
    await typeUntil(app, "id:composer", "One more thing", "id:send");
    await pressWhenEnabled(app, "id:send");
    await app.waitUntil(() => framesOf(app.host.posts(), "send_input").length === 1);
    await app.wait(500);
    for (let i = 0; i < 3; i += 1) await app.scroll("id:session-chat", -1500);
    await app.wait(400);
    expect(app.shows("id:jump-latest")).toBe(true);
    // Read back up, the capsule has collapsed: a tap on its name brings its view switch back.
    await app.press("id:capsule-name");
    await app.press("id:session-mode");
    await app.waitUntil(() => !app.shows("id:session-chat"));
    // The terminal has it collapsed again: the name first.
    await app.press("id:capsule-name");
    await app.press("id:session-mode");
    await app.waitFor("id:session-chat");
    await app.wait(600);
    // Back where it was read to (its saved row), not at the message sent before, near the end.
    expect(app.shows("id:jump-latest")).toBe(true);
  }, 40_000);
});

describe("a row whose content changes shows the change where it is", () => {
  test("a tool row goes from running to failed, and a reply's words grow, in the rows already drawn", async () => {
    const session = { id: "w1", name: "tools", status: "running", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
    const t = (i: number) => new Date(Date.UTC(2026, 10, 13, 2, 0, i)).toISOString();
    const run = { id: "c1", seq: 2, kind: "command", timestamp: t(1), command: "bun test settings", status: "running" };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: [{ id: "u1", seq: 1, kind: "user_message", timestamp: t(0), body: "Run the tests" }, run] } }) });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("text:Run the tests", { timeout: 10_000 });
    await app.wait(300);
    const running = app.texts();
    await app.host.send("app.conversation", { worker: "w1", upserts: [{ ...run, status: "failed", exit_code: 1, output: "1 fail" }] });
    expect(running).toContain("Run bun test settings");
    await app.waitFor("text:Run bun test settings  Failed · exit 1");
    // A reply streams into the row already drawn.
    for (const body of ["Two", "Two tests failed", "Two tests failed: settings and sync."]) {
      await app.host.send("app.conversation", { worker: "w1", upserts: [{ id: "a1", seq: 3, kind: "assistant_message", timestamp: t(2), body }] });
      await app.waitFor(`text:${body}`);
    }
  }, 30_000);
});

describe("Brain's host restarts (the same thread under a new host id)", () => {
  test("the thread read back up stays drawn and in its place while the new host's copy loads", async () => {
    const app = await readBackUp();
    const before = firstDrawn(app);
    const builtBefore = framesOf(app.host.posts(), "codex_conversation_subscribe").length;
    await app.host.send("app.daemonPush", { type: "brain_snapshot", brain: { ...brain, host_worker: { ...host, id: "brain-host-2" } } });
    await app.waitUntil(() => framesOf(app.host.posts(), "codex_conversation_subscribe").length > builtBefore, { timeout: 5_000 });
    await app.wait(500);
    expect(app.shows("id:jump-latest")).toBe(true);
    expect(firstDrawn(app)).toBe(before);
  }, 30_000);
});

describe("Brain's other threads", () => {
  test("a past thread opens at its own end, and Current chat comes back to the current one", async () => {
    const past = Array.from({ length: 30 }, (_, i) => ({
      id: `old${i}`, seq: i + 1, kind: i % 2 ? "assistant_message" : "user_message",
      timestamp: new Date(Date.UTC(2026, 10, 1, 7, 0, i)).toISOString(), body: `Old ${i}. ${"Words of an older thread that wrap in a phone-sized chat. ".repeat(4)}`,
    }));
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": long, "brain-host#brain-thread:t-old": past } }) });
    await app.waitFor("id:brain-chat", { timeout: 10_000 });
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 59.")), { timeout: 10_000 });
    await app.host.send("links.open", { url: "mewla://brain?thread=t-old" });
    await app.waitFor("id:brain-past-thread");
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Old 29.")), { timeout: 10_000 });
    await app.wait(400);
    expect(app.shows("id:jump-latest")).toBe(false);
    await app.press("id:brain-current-thread");
    await app.waitUntil(() => app.texts().some((t) => t.startsWith("Message 59.")), { timeout: 10_000 });
    await app.wait(400);
    expect(app.shows("id:jump-latest")).toBe(false);
  }, 30_000);
});
