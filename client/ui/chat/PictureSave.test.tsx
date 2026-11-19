import { describe, expect, open, test } from "nsh/test";
import { framesOf, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
// Two different originals: a 1×1 PNG and a 1×1 GIF, so a test can tell which bytes went where.
const PNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
const GIF = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7";
const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
const brain = { chat_thread_id: "t-now", host_worker };
const at = (s: number) => `2026-11-12T07:00:${String(s).padStart(2, "0")}.000Z`;
// The owner's case first: a picture Brain's Read returned, under its tool row; then a sent one and a reply's.
const thread = [
  { id: "u1", seq: 1, timestamp: at(1), kind: "user_message", role: "user", body: "What is wrong here?",
    attachments: [{ name: "shot.png", path: "/state/uploads/shot.png", content_type: "image/png" }] },
  { id: "t1", seq: 2, timestamp: at(2), kind: "tool_call", role: "assistant", call_id: "c1", tool_name: "Read", title: "Read screen.png", input: JSON.stringify({ file_path: "/tmp/screen.png" }), status: "completed" },
  { id: "a1", seq: 3, timestamp: at(3), kind: "assistant_message", role: "assistant", body: "The button is cut off:\n\n![the login page](/home/me/shots/login.jpg)\n" },
];
const assets = { "/state/uploads/shot.png": PNG, "/tmp/screen.png": PNG, "/home/me/shots/login.jpg": GIF };

/** Brain's chat on a phone whose host the test plays: what it was handed to save and to share. */
async function phone(options: { platform?: "android" | "ios"; save?: (data: any) => unknown; old?: boolean } = {}) {
  const saved: any[] = [];
  const shared: any[] = [];
  const save = options.save ?? ((data: any) => (saved.push(data), { name: data.name, where: options.platform === "ios" ? "Photos" : "Pictures/Mewla" }));
  const pictureCalls: Record<string, (data: any) => unknown> = options.old ? {} : { "files.canSaveImage": () => true, "files.saveImage": save, "files.shareImage": (data: any) => (shared.push(data), true) };
  const app = await open(APP, {
    platform: options.platform ?? "android",
    storage: paired,
    host: phoneHost({ brain, conversations: { "brain-host": thread }, assets }, pictureCalls),
  });
  await app.waitFor("id:composer", { timeout: 10_000 });
  await app.waitFor("id:tool-pictures >> id:chat-image-open", { timeout: 10_000 });
  await app.wait(300);
  const fullAsks = () => framesOf(app.host.posts(), "codex_asset").filter((frame) => frame.max_edge === undefined).map((frame) => frame.path);
  return { app, saved, shared, fullAsks };
}

describe("saving a picture from the viewer", () => {
  test("a tool call's picture (Brain's Read): Save puts the original's bytes in the gallery under its own name", async () => {
    const { app, saved, fullAsks } = await phone();
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("text:2 of 3");
    await app.waitFor("id:picture-viewer-image");
    expect(app.shows("id:picture-viewer-save")).toBe(true);
    expect(app.shows("id:picture-viewer-share")).toBe(true);
    // Android's clipboard holds no pictures: no Copy there.
    expect(app.shows("id:picture-viewer-copy")).toBe(false);
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Saved to Pictures/Mewla");
    expect(fullAsks()).toContain("/tmp/screen.png");
    // The host gets the original whole, as the computer read it.
    expect(saved).toEqual([{ source: PNG, name: "screen.png", mimeType: "image/png" }]);
  }, 20_000);

  test("a reply's picture saves under its name with the extension of what it really is", async () => {
    const { app, saved } = await phone();
    await app.press("id:chat-image-open >> nth=2");
    await app.waitFor("text:3 of 3");
    await app.waitFor("id:picture-viewer-image");
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Saved to Pictures/Mewla");
    // login.jpg is a GIF: it saves as login.gif, its own bytes.
    expect(saved.at(-1)).toEqual({ source: GIF, name: "login.gif", mimeType: "image/gif" });
  }, 20_000);

  test("a sent picture saves too", async () => {
    const { app, saved } = await phone();
    await app.press("id:sent-pictures >> id:chat-image-open");
    await app.waitFor("text:1 of 3");
    await app.waitFor("id:picture-viewer-image");
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Saved to Pictures/Mewla");
    expect(saved.at(-1)).toMatchObject({ name: "shot.png", mimeType: "image/png" });
  }, 20_000);

  test("Share hands the original as a file to the share sheet", async () => {
    const { app, shared } = await phone();
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-image");
    await app.press("id:picture-viewer-share");
    await app.waitUntil(() => shared.length === 1, { timeout: 5_000 });
    expect(shared[0]).toEqual({ source: PNG, name: "screen.png", mimeType: "image/png" });
  }, 20_000);

  test("a save that failed says what failed", async () => {
    const { app } = await phone({ save: () => { throw new Error("The gallery did not take the picture."); } });
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-image");
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Couldn't save the picture: The gallery did not take the picture");
  }, 20_000);

  test("an app build from before Save shows the viewer as it was, and a hold offers nothing", async () => {
    const { app, saved } = await phone({ old: true });
    let asked = false;
    app.onDialog(() => ((asked = true), "Save"));
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-image");
    await app.wait(300);
    expect(app.shows("id:picture-viewer-save")).toBe(false);
    expect(app.shows("id:picture-viewer-share")).toBe(false);
    await app.press("id:picture-viewer-close");
    await app.waitUntil(() => !app.shows("id:picture-viewer-count"));
    await app.longPress("id:tool-pictures >> id:chat-image-open");
    await app.wait(300);
    expect(asked).toBe(false);
    expect(saved).toEqual([]);
  }, 20_000);

  test("on iOS it saves to Photos and can copy", async () => {
    const { app, saved } = await phone({ platform: "ios" });
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-image");
    expect(app.shows("id:picture-viewer-copy")).toBe(true);
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Saved to Photos");
    expect(saved).toHaveLength(1);
  }, 20_000);
});

describe("holding a picture in the thread", () => {
  test("offers Save and Share; Save reads the original, never the row's phone-sized copy", async () => {
    const { app, saved, fullAsks } = await phone();
    let sheet: string[] = [];
    app.onDialog((dialog) => {
      sheet = dialog.buttons;
      return "Save";
    });
    expect(fullAsks()).not.toContain("/tmp/screen.png");
    await app.longPress("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("text:Saved to Pictures/Mewla");
    expect(sheet).toEqual(["Save", "Share", "Cancel"]);
    expect(fullAsks()).toContain("/tmp/screen.png");
    expect(saved).toEqual([{ source: PNG, name: "screen.png", mimeType: "image/png" }]);
  }, 20_000);
});

describe("in a browser", () => {
  test("Download hands the original to the browser's download; Copy puts it on the clipboard", async () => {
    const downloads: any[] = [];
    const app = await open(APP, {
      platform: "web",
      storage: paired,
      host: phoneHost({ brain, conversations: { "brain-host": thread }, assets }, { "files.canSaveImage": () => true, "files.saveImage": (data: any) => (downloads.push(data), { name: data.name }) }),
    });
    await app.waitFor("id:tool-pictures >> id:chat-image-open", { timeout: 10_000 });
    await app.wait(300);
    await app.press("id:tool-pictures >> id:chat-image-open");
    await app.waitFor("id:picture-viewer-image");
    expect(app.shows("id:picture-viewer-share")).toBe(false);
    await app.press("id:picture-viewer-save");
    await app.waitFor("text:Downloaded");
    expect(downloads).toEqual([{ source: PNG, name: "screen.png", mimeType: "image/png" }]);
    await app.press("id:picture-viewer-copy");
    await app.waitFor("text:Copied");
  }, 20_000);
});
