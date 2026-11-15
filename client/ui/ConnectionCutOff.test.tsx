import { describe, expect, open, test, type App } from "nsh/test";
import { paired, phoneHost } from "./testing/fixtures";

/**
 * Owner 11-15 (v0.4.5): back in Mewla after a while in another app, messages did not load and Stats showed nothing
 * until they switched computers; the computer saw no socket from the phone for 9 min. A phone back from the
 * background is handed the frames queued behind the freeze and the end of a socket the computer already dropped in
 * one call, and handing those frames over can take the call past NSH's 2 s: NSH stops it there, no catch or finally
 * runs, and the Transport, cut off before it armed its reconnect, stayed "connected" with no socket for good. Here a
 * thread update too big for 2 s arrives with the drop in one call; the app must connect again and show the next news.
 */
const APP = "./FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const ts = (i: number) => new Date(Date.UTC(2026, 10, 11, 7, 0, 0) + i * 1000).toISOString();
const say = (i: number, body: string) => ({ id: `m${i}`, seq: i, kind: i % 2 ? "assistant_message" : "user_message", role: i % 2 ? "assistant" : "user", timestamp: ts(i), body });
const output = Array.from({ length: 60 }, (_, i) => `line ${i}: ok some output text here padding....`).join("\n");
// Some 4 s of projection on a dev box: well past one call's budget (ui/chat/ThreadCutOff.test.tsx).
const tooMuch = Array.from({ length: 4000 }, (_, i) => ({
  id: `h${i}`, seq: 100 + i, kind: "tool", title: "Tool", tool_name: "Read", input: JSON.stringify({ file_path: `/x/${i}` }), output, status: "done", timestamp: ts(100 + i),
}));
const sockets = (app: App) => Number(app.storage()["fake:sockets"] ?? 0);

describe("a drop whose call NSH cuts off", () => {
  test("the app connects again and shows the computer's next news", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": [say(0, "Message 0."), say(1, "Message 1.")] } }) });
    await app.waitFor("text:Message 1.", { timeout: 10_000 });
    expect(sockets(app)).toBe(1);
    try {
      await app.host.send("app.daemonNewsThenDrop", { worker: "brain-host", upserts: tooMuch });
      await app.wait(3000);
    } catch (error) {
      expect(String(error)).toContain("interrupted");
    }
    await app.waitUntil(() => sockets(app) >= 2, { timeout: 10_000 });
    await app.host.send("app.conversation", { worker: "brain-host", deletes: tooMuch.map((event) => event.id), upserts: [say(5001, "The answer, after it.")] });
    await app.waitFor("text:The answer, after it.", { timeout: 10_000 });
  }, 60_000);
});
