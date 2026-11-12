import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const PIXEL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const at = (s: number) => `2026-11-12T07:00:${String(s).padStart(2, "0")}.000Z`;
const thread = [
  { id: "u1", seq: 1, timestamp: at(1), kind: "user_message", role: "user", body: "What is wrong here?",
    attachments: [{ name: "shot.png", path: "/state/uploads/shot.png", content_type: "image/png" }, { name: "notes.pdf", path: "/state/uploads/notes.pdf", content_type: "application/pdf" }] },
  { id: "t1", seq: 2, timestamp: at(2), kind: "tool_call", role: "assistant", call_id: "c1", tool_name: "Read", title: "Read screen.png", input: JSON.stringify({ path: "/tmp/screen.png" }), status: "completed" },
  { id: "a1", seq: 3, timestamp: at(3), kind: "assistant_message", role: "assistant", body: "The button is cut off:\n\n![the login page](/home/me/shots/login.png)\n" },
];

async function brainWithPictures() {
  const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": thread }, replies: { codex_asset: { type: "codex_asset", data_url: PIXEL } } }) });
  await app.waitFor("id:composer", { timeout: 10_000 });
  await app.waitFor("id:sent-pictures", { timeout: 10_000 });
  return app;
}

describe("pictures in a chat", () => {
  test("a sent picture shows as a picture above the message; a file stays a row in it", async () => {
    const app = await brainWithPictures();
    expect(app.count("id:sent-pictures >> id:chat-image")).toBe(1);
    expect(app.texts()).toContain("notes.pdf");
    expect(app.texts()).not.toContain("shot.png");
  }, 20_000);

  test("a tool call's picture is a thumbnail under its row", async () => {
    const app = await brainWithPictures();
    await app.waitFor("id:tool-pictures >> id:chat-image-open", { timeout: 10_000 });
  }, 20_000);

  test("a tap on a picture opens the viewer on it, which steps through every picture of the thread in order", async () => {
    const app = await brainWithPictures();
    await app.waitFor("id:tool-pictures >> id:chat-image-open", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-count");
    expect(app.texts()).toContain("2 of 3");
    await app.waitFor("id:picture-viewer-image");
    await app.press("id:picture-viewer-next");
    await app.waitFor("text:3 of 3");
    expect(app.enabled("id:picture-viewer-next")).toBe(false);
    await app.keyboard.press("left");
    await app.waitFor("text:2 of 3");
    await app.press("id:picture-viewer-previous");
    await app.waitFor("text:1 of 3");
    expect(app.enabled("id:picture-viewer-previous")).toBe(false);
    await app.press("id:picture-viewer-close");
    await app.waitUntil(() => !app.shows("id:picture-viewer-count"));
  }, 20_000);

  test("a reply's picture opens the viewer too, and Back closes it", async () => {
    const app = await brainWithPictures();
    await app.waitFor("label:the login page", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:chat-image-open >> nth=2");
    await app.waitFor("text:3 of 3");
    await app.back();
    await app.waitUntil(() => !app.shows("id:picture-viewer-count"));
    expect(app.shows("id:composer")).toBe(true);
  }, 20_000);

  test("a swipe in the viewer goes to the next picture", async () => {
    const app = await brainWithPictures();
    await app.waitFor("id:sent-pictures >> id:chat-image-open", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:sent-pictures >> id:chat-image-open");
    await app.waitFor("text:1 of 3");
    await app.scroll("label:Pictures in this chat", 400);
    await app.waitFor("text:2 of 3");
  }, 20_000);
});

describe("a picture that failed to load", () => {
  test("keeps its frame with a refresh glyph, and a tap loads it again", async () => {
    let asked = 0;
    const app = await open("./testing/Picture.tsx", { platform: "android", host: { handlers: { "app.picture": () => (++asked === 1 ? null : PIXEL) } } });
    await app.waitFor("id:chat-image-retry");
    await app.press("id:chat-image-retry");
    await app.waitFor("id:chat-image-open");
    expect(asked).toBe(2);
  });
});
