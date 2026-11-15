import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "./testing/fixtures";

/**
 * Owner's PYR110 (11-15): many NSH 2 s cut-offs a day, their frames cheap code. NSH counts the time the system keeps
 * the app frozen in the background against the call it froze (longbridge/nsh sandbox.rs: a monotonic deadline), so a
 * call caught by a freeze is cut off on thaw. Out of sight the app now runs only what has to work there (the live
 * notification and widgets, a notification's answer, what waits on its own requests): the computer's news waits for
 * the app to come back (core/transport.ts `setAway`).
 */
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const ts = (i: number) => new Date(Date.UTC(2026, 10, 11, 7, 0, 0) + i * 1000).toISOString();
const say = (i: number, body: string) => ({ id: `m${i}`, seq: i, kind: i % 2 ? "assistant_message" : "user_message", role: i % 2 ? "assistant" : "user", timestamp: ts(i), body });

describe("out of sight", () => {
  test("the thread's news waits for the app to be back; the live notification keeps up meanwhile", async () => {
    const applied: string[] = [];
    const app = await open("./FakeDaemonApp.tsx", {
      platform: "android", storage: paired,
      host: phoneHost({ brain, conversations: { "brain-host": [say(0, "Message 0."), say(1, "Message 1.")] } }, {
        "live.apply": ({ state }: { state: string }) => void applied.push(state),
      }),
    });
    await app.waitFor("text:Message 1.", { timeout: 10_000 });
    const before = applied.length;
    await app.setLifecycle("background");
    await app.host.send("app.conversation", { worker: "brain-host", upserts: [say(3, "Said while away.")] });
    await app.host.send("app.daemonPush", { type: "live_state", live: { title: "While away" } });
    await app.wait(500);
    expect(applied.slice(before).some((state) => state.includes("While away"))).toBe(true);
    expect(app.texts().join("\n")).not.toContain("Said while away.");
    await app.setLifecycle("active");
    await app.waitFor("text:Said while away.", { timeout: 10_000 });
  }, 60_000);
});
