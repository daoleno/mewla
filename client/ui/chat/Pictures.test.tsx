import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

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

  test("a row asks the computer for its picture phone-sized; the viewer asks for the original", async () => {
    const app = await brainWithPictures();
    await app.waitFor("label:the login page", { timeout: 10_000 });
    const asks = () => framesOf(app.host.posts(), "codex_asset").map((frame) => `${frame.path} ${frame.max_edge ?? "full"}`);
    await app.waitUntil(() => asks().includes("/home/me/shots/login.png 1600"), { timeout: 10_000 });
    expect(asks().every((ask) => ask.endsWith(" 1600"))).toBe(true);
    await app.wait(300);
    await app.press("id:chat-image-open >> nth=2");
    await app.waitFor("text:3 of 3");
    await app.waitUntil(() => asks().includes("/home/me/shots/login.png full"), { timeout: 10_000 });
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

describe("the viewer while the thread moves", () => {
  test("a reply streaming meanwhile leaves the viewer on its picture; a new picture joins the count", async () => {
    const app = await brainWithPictures();
    await app.waitFor("id:tool-pictures >> id:chat-image-open", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-image");
    const subscription = framesOf(app.host.posts(), "codex_conversation_subscribe").filter((frame) => frame.worker_id === "brain-host").at(-1)!;
    const delta = (revision: number, body: string) => app.host.send("app.daemonPush", { type: "codex_conversation_delta", request_id: subscription.request_id, worker_id: "brain-host", revision, base_revision: revision - 1, deletes: [],
      upserts: [{ id: "a2", seq: 4, timestamp: at(4), kind: "assistant_message", role: "assistant", body, streaming: true }] });
    await delta(2, "Looking at");
    await delta(3, "Looking at the second one");
    await app.wait(300);
    expect(app.texts()).toContain("2 of 3");
    expect(app.shows("id:picture-viewer-image")).toBe(true);
    await delta(4, "Looking at the second one: ![zoomed](/home/me/shots/zoom.png)");
    await app.waitFor("text:2 of 4");
  }, 20_000);
});

describe("a picture whose read the link interrupted", () => {
  test("waits for the link and shows by itself: no refresh glyph, no tap", async () => {
    const app = await open(APP, { platform: "android", storage: paired, host: phoneHost({ brain, conversations: { "brain-host": thread }, replies: { codex_asset: { type: "codex_asset", data_url: PIXEL } }, assetsLost: 1 }) });
    await app.waitFor("id:sent-pictures", { timeout: 10_000 });
    await app.waitFor("id:sent-pictures >> id:chat-image-open", { timeout: 10_000 });
    expect(app.count("id:chat-image-retry")).toBe(0);
  }, 20_000);
});

describe("a picture whose read did not answer", () => {
  test("shows a placeholder, not the refresh glyph, and reads again by itself", async () => {
    let asked = 0;
    const app = await open("./testing/Picture.tsx", { platform: "android", host: { handlers: { "app.picture": () => {
      if (++asked === 1) throw new Error("codex_asset timed out");
      return PIXEL;
    } } } });
    await app.waitFor("id:chat-image-loading");
    expect(app.count("id:chat-image-retry")).toBe(0);
    await app.waitFor("id:chat-image-open", { timeout: 5_000 });
    expect(asked).toBe(2);
    expect(app.count("id:chat-image-retry")).toBe(0);
  });

  test("shows the refresh glyph only after its reads again went unanswered (1, 2, 4, 8 s)", async () => {
    let asked = 0;
    const app = await open("./testing/Picture.tsx", { platform: "android", host: { handlers: { "app.picture": () => {
      asked++;
      throw new Error("codex_asset timed out");
    } } } });
    await app.wait(13_000);
    expect(app.count("id:chat-image-retry")).toBe(0);
    expect(app.shows("id:chat-image-slow-on")).toBe(true);
    await app.waitFor("id:chat-image-retry", { timeout: 5_000 });
    expect(asked).toBe(5);
  }, 25_000);
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
