import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost, pressWhenEnabled, typeUntil } from "../testing/fixtures";

/** SPEC §16.4 Sending: optimistic, at full strength; a clock beside it only once it is slow; never "Not sent" early. */
const APP = "../FakeDaemonApp.tsx";
const host = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" };
const brain = { chat_thread_id: "t-now", host_worker: host };
const PIXEL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

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

  test("a message with a picture whose answer was lost with the link is sent again under its id and shows once", async () => {
    // The computer takes it and echoes it, but the link drops before its answer; the resend is answered, not stored again.
    const app = await open(APP, {
      platform: "android", storage: paired,
      host: phoneHost({ brain, conversations: { "brain-host": [] }, admitsInput: { loseFirstAck: true }, replies: { codex_asset: { type: "codex_asset", data_url: PIXEL } } }),
      network: { "POST http://127.0.0.1:1/upload": () => ({ status: 200, body: JSON.stringify({ path: "/state/uploads/working.webp", name: "working.webp", content_type: "image/webp" }) }) },
    });
    await app.waitFor("id:composer", { timeout: 10_000 });
    await app.wait(500);
    await app.paste("id:composer", { files: ["assets/pets/p13/working.webp"] });
    await app.waitFor("text:Ready", { timeout: 10_000 });
    await typeUntil(app, "id:composer", "Why does it crash?", "id:send");
    await pressWhenEnabled(app, "id:send");
    const sends = () => framesOf(app.host.posts(), "send_input");
    await app.waitUntil(() => sends().length >= 2, { timeout: 10_000 });
    expect(new Set(sends().map((frame) => frame.request_id)).size).toBe(1);
    expect((sends()[0]!.attachments as { path: string }[]).map((file) => file.path)).toEqual(["/state/uploads/working.webp"]);
    await app.waitFor("id:sent-pictures >> id:chat-image", { timeout: 10_000 });
    await app.wait(1_000);
    expect(app.texts().filter((text) => text === "Why does it crash?").length).toBe(1);
    expect(app.count("id:sent-pictures >> id:chat-image")).toBe(1);
  }, 30_000);
});
