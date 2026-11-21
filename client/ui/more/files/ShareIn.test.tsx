import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost, stepUntil } from "../../testing/fixtures";

// What other apps share to Mewla (Android: share.take / share.ack / share.release, the share.arrived event) on the
// whole app: it comes to Brain's chat, the files are chips that upload at once, the text joins the draft.
const APP = "../../FakeDaemonApp.tsx";
const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 } };

type Batch = { id: string; text: string; problem: string | null; assets: Array<Record<string, unknown>> };
const file = (name: string, extra: Record<string, unknown> = {}) => ({ uri: `file:///cache/share-in/b1/${name}`, name, mimeType: "application/pdf", size: 4096, ...extra });

function phone(waiting: Batch[], extra: Record<string, (data: any) => unknown> = {}) {
  const state = { waiting, acked: [] as string[], released: [] as string[], uploaded: [] as string[] };
  const host = phoneHost({ brain }, {
    "share.take": () => state.waiting.filter((batch) => !state.acked.includes(batch.id)),
    "share.ack": ({ ids }: { ids: string[] }) => void state.acked.push(...ids),
    "share.release": ({ uris }: { uris: string[] }) => void state.released.push(...uris),
    "files.upload": ({ fileUri }: { fileUri: string }) => {
      state.uploaded.push(fileUri);
      return { status: 200, body: JSON.stringify({ path: `/state/uploads/${fileUri.split("/").pop()}`, name: fileUri.split("/").pop() }) };
    },
    "files.thumbnail": () => null,
    "files.cancel": () => null,
    ...extra,
  });
  return { state, host };
}

describe("a share from another app", () => {
  test("waiting at start (a cold start): lands on Brain as chips that upload at once, and the host is told", async () => {
    const p = phone([{ id: "b1", text: "", problem: null, assets: [file("report.pdf"), file("slides.pdf")] }]);
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("id:attachment-upload-1", { timeout: 15_000 });
    await app.waitFor("id:attachment-upload-2");
    await app.waitUntil(() => app.texts().filter((text) => text === "Ready").length === 2, { timeout: 10_000 });
    expect(p.state.uploaded).toEqual(["file:///cache/share-in/b1/report.pdf", "file:///cache/share-in/b1/slides.pdf"]);
    expect(p.state.acked).toEqual(["b1"]);
    // Nothing is sent by itself.
    expect(app.texts()).toContain("report.pdf");
  }, 30_000);

  test("shared text joins the draft below what was typed there, and a second share is added to the first", async () => {
    const p = phone([]);
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.type("id:composer", "see this");
    p.state.waiting = [{ id: "b1", text: "Title\nhttps://example.com/a", problem: null, assets: [file("a.pdf")] }];
    await app.host.send("share.arrived", {});
    await app.waitFor("id:attachment-upload-1", { timeout: 10_000 });
    await app.waitUntil(() => app.value("id:composer") === "see this\n\nTitle\nhttps://example.com/a", { timeout: 10_000 });
    p.state.waiting = [...p.state.waiting, { id: "b2", text: "", problem: null, assets: [file("b.pdf")] }];
    await app.host.send("share.arrived", {});
    await app.waitFor("id:attachment-upload-2", { timeout: 10_000 });
    expect(app.shows("id:attachment-upload-1")).toBe(true);
    expect(p.state.acked).toEqual(["b1", "b2"]);
  }, 30_000);

  test("arriving while a Session is open brings Brain's chat to the front, its draft kept", async () => {
    const session = { id: "w1", name: "notes", status: "running", cwd: "/home/me/mewla", command: "claude", provider: "claude", started_at: 1_762_840_800_000, process_id: 77, updated_at: Date.now() };
    const transcript = [{ id: "u1", seq: 1, kind: "user_message", timestamp: "2026-11-11T07:00:00.000Z", body: "Write the plan" }];
    const p = phone([], {});
    p.host.params = { daemon: { brain, worker_sessions: [session], conversations: { w1: transcript } } } as never;
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("id:composer", { timeout: 15_000 });
    await app.type("id:composer", "half a thought");
    await app.waitUntil(() => app.value("id:composer") === "half a thought");
    await app.host.send("links.open", { url: "mewla://sessions/w1" });
    await app.waitFor("text:Write the plan", { timeout: 10_000 });
    p.state.waiting = [{ id: "b1", text: "", problem: null, assets: [file("report.pdf")] }];
    await app.host.send("share.arrived", {});
    await app.waitFor("id:home-header", { timeout: 10_000 });
    await app.waitFor("id:attachment-upload-1", { timeout: 10_000 });
    expect(app.value("id:composer")).toBe("half a thought");
  }, 30_000);

  test("removing a chip gives the host's copy back", async () => {
    const p = phone([{ id: "b1", text: "", problem: null, assets: [file("report.pdf")] }]);
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("id:attachment-upload-1-remove", { timeout: 15_000 });
    await app.press("id:attachment-upload-1-remove");
    await stepUntil(app, () => p.state.released.length === 1);
    expect(p.state.released).toEqual(["file:///cache/share-in/b1/report.pdf"]);
  }, 30_000);

  test("a file the host could not read is a failed chip that says why, with no Retry", async () => {
    const bad = file("gone.pdf", { selectionError: "Cannot read this file. Access was denied or it is gone.", selectionRetryable: false });
    const p = phone([{ id: "b1", text: "", problem: null, assets: [bad] }]);
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("text:Cannot read this file. Access was denied or it is gone.", { timeout: 15_000 });
    expect(app.shows("id:attachment-upload-1-retry")).toBe(false);
    expect(p.state.uploaded).toEqual([]);
  }, 30_000);

  test("a file over the limit answers with the app's own message", async () => {
    const huge = file("movie.mkv", { size: 3 * 1024 * 1024 * 1024 });
    const p = phone([{ id: "b1", text: "", problem: null, assets: [huge] }]);
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("text:File exceeds the 2 GiB upload limit.", { timeout: 15_000 });
    expect(p.state.uploaded).toEqual([]);
  }, 30_000);

  test("a share with nothing usable says so where attachment errors show", async () => {
    const p = phone([{ id: "b1", text: "", problem: "Nothing usable was shared with Mewla.", assets: [] }]);
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("text:Nothing usable was shared with Mewla.", { timeout: 15_000 });
    expect(p.state.acked).toEqual(["b1"]);
  }, 30_000);

  test("a host that does not know share.take (iOS, an older app) leaves the app as it was", async () => {
    const p = phone([], { "share.take": () => { throw new Error("No handler"); } });
    const app = await open(APP, { host: p.host, storage: paired, platform: "android" });
    await app.waitFor("id:composer", { timeout: 15_000 });
    expect(app.shows("id:attachment-upload-1")).toBe(false);
  }, 30_000);
});
