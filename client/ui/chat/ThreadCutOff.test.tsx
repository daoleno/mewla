import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost, pressWhenEnabled, typeUntil } from "../testing/fixtures";

/**
 * Owner 11-14 (v0.4.2): Brain's chat showed "Working · 4m" over an answer the terminal already had, and showed
 * nothing new after it, not the person's next message, not after the app reconnected, until the app started again.
 * The phone had taken more than 2 s over one update of the thread, and NSH cut that call off ("Error: interrupted")
 * inside the thread's projection, which was a `computed`: one that throws never updates what reads it again
 * (kit/derived.test.tsx). Here an update with thousands of tool rows takes the projection past 2 s on purpose; the
 * update after it, which takes them away, must reach the screen.
 */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const ts = (i: number) => new Date(Date.UTC(2026, 10, 11, 7, 0, 0) + i * 1000).toISOString();
const say = (i: number, body: string) => ({ id: `m${i}`, seq: i, kind: i % 2 ? "assistant_message" : "user_message", role: i % 2 ? "assistant" : "user", timestamp: ts(i), body });
const output = Array.from({ length: 60 }, (_, i) => `line ${i}: ok some output text here padding....`).join("\n");
// Some 4 s of projection on a dev box: well past one call's budget.
const tooMuch = Array.from({ length: 4000 }, (_, i) => ({
  id: `h${i}`, seq: 100 + i, kind: "tool", title: "Tool", tool_name: "Read", input: JSON.stringify({ file_path: `/x/${i}` }), output, status: "done", timestamp: ts(100 + i),
}));

/** Sends the update the projection cannot finish in time: the app throws "interrupted", as the phone did. */
async function cutOff(app: Awaited<ReturnType<typeof open>>, worker: string) {
  try {
    await app.host.send("app.conversation", { worker, upserts: tooMuch });
    await app.wait(3000);
  } catch (error) {
    expect(String(error)).toContain("interrupted");
  }
}

async function nextNewsShows(app: Awaited<ReturnType<typeof open>>, worker: string) {
  await app.host.send("app.conversation", { worker, deletes: tooMuch.map((event) => event.id), upserts: [say(5001, "The answer, after it.")] });
  await app.waitFor("text:The answer, after it.", { timeout: 8000 });
  await typeUntil(app, "id:composer", "Ship it tonight.", "id:send");
  await pressWhenEnabled(app, "id:send");
  await app.waitUntil(() => app.value("id:composer") === "" && app.texts().includes("Ship it tonight."), { timeout: 8000 });
}

describe("a thread update cut off by NSH's 2 s budget", () => {
  test("Brain's thread draws the next news and the person's next message", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": [say(0, "Message 0."), say(1, "Message 1.")] } }) });
    await app.waitFor("text:Message 1.", { timeout: 10_000 });
    await cutOff(app, "brain-host");
    await nextNewsShows(app, "brain-host");
  }, 60_000);

  test("a Session's chat draws the next news and the person's next message", async () => {
    const session = { id: "w1", name: "charts", status: "running", cwd: "/home/me/mewla", command: "claude", provider: "claude", updated_at: Date.now() };
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, worker_sessions: [session], conversations: { "brain-host": [], w1: [say(0, "Message 0."), say(1, "Message 1.")] } }) });
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("text:Message 1.", { timeout: 10_000 });
    await cutOff(app, "w1");
    await nextNewsShows(app, "w1");
  }, 60_000);
});
