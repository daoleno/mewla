import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressWhenEnabled, typeUntil } from "../testing/fixtures";

/** SPEC §16.4 Sending: optimistic, at full strength; a clock beside it only once it is slow; never "Not sent" early. */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };

describe("sending a message", () => {
  test("it is in its place at once with nothing else; the clock beside it from 3 s while the computer has not echoed it", async () => {
    // The computer takes the message and never echoes it (no `send_input` answer).
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": [] } }) });
    await app.waitFor("id:composer", { timeout: 10_000 });
    await typeUntil(app, "id:composer", "Ship it tonight.", "id:send");
    await pressWhenEnabled(app, "id:send");
    await app.waitFor("text:Ship it tonight.");
    await app.waitUntil(() => framesOf(app.host.posts(), "send_input").length > 0, { timeout: 5_000 });
    expect(app.shows("id:message-slow")).toBe(false);
    await app.waitFor("id:message-slow", { timeout: 6_000 });
    expect(app.texts().some((text) => /Not sent/.test(text))).toBe(false);
  }, 25_000);
});
