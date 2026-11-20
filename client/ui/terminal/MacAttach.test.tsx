import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";

// Mewla for Mac's terminal Attach (DESIGN-MAC.md §2.3.1): the header's first action picks with NSH's own picker (the
// Mac's host answers no files.*), the file uploads as the app's own, chips float over the grid, and the quoted path
// is typed at the prompt.
const APP = "../FakeDaemonApp.tsx";
const UPLOAD = "POST http://127.0.0.1:1/upload";
const pairing = { expires_at: "2099-01-01T00:00:00Z", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64), local_link: "mewla://settings?p=local" };
const shell = { id: "w6", name: "shell", status: "running", cwd: "/home/me/mewla", command: "zsh", provider: "", updated_at: Date.now() };
const file = { name: "plan notes.pdf", type: "application/pdf", data: new Uint8Array([37, 80, 68, 70]) };
const done = { status: 200, body: JSON.stringify({ path: "/state/uploads/plan notes.pdf", name: "plan notes.pdf" }) };

async function terminal(upload: () => unknown) {
  const host = phoneHost({ brain: {}, worker_sessions: [shell], conversations: { w6: [] } }, { "app.pairing": () => pairing });
  const app = await open(APP, { platform: "macos", width: 1280, height: 800, storage: paired, host, network: { [UPLOAD]: upload as never } });
  await app.host.send("links.open", { url: "mewla://sessions/w6" });
  await app.waitFor("id:session-attach", { timeout: 10_000 });
  app.onPick(() => [file]);
  return app;
}

const uploads = (app: Awaited<ReturnType<typeof open>>) => app.requests().filter((request) => `${request.method} ${request.url}` === UPLOAD);

// Headless, the Mac's terminal (a web view) never attaches, so the typed path is proven on a Mac (macos-app.yml).
describe("Mewla for Mac's terminal Attach", () => {
  test("the header action picks with NSH's picker, uploads the file as the app's own, and the chip leaves once it is up", async () => {
    const app = await terminal(() => done);
    await app.press("id:session-attach");
    await app.waitUntil(() => uploads(app).length === 1, { timeout: 10_000 });
    expect(app.picks().at(-1)).toMatchObject({ kind: "files", multiple: true });
    expect(Object.entries(uploads(app)[0]!.headers).find(([key]) => key.toLowerCase() === "x-mewla-upload-name")?.[1]).toContain("plan");
    await app.waitFor("text:plan notes", { state: "gone" });
  }, 30_000);

  test("a failed upload stays as a chip over the grid with its error and Retry, and Retry sends it again", async () => {
    let calls = 0;
    const app = await terminal(() => (++calls === 1 ? { status: 500, body: "disk full" } : done));
    await app.press("id:session-attach");
    await app.waitFor("id:terminal-attach-chips >> text:Retry", { timeout: 10_000 });
    await app.press("text:Retry");
    await app.waitUntil(() => calls === 2, { timeout: 10_000 });
    await app.waitFor("text:plan notes", { state: "gone" });
  }, 30_000);

  test("Cancel while it uploads drops the chip", async () => {
    const app = await terminal(() => ({ ...done, delay: 8000 }));
    await app.press("id:session-attach");
    await app.waitFor("id:terminal-attach-chips >> text:Cancel", { timeout: 10_000 });
    await app.press("text:Cancel");
    await app.waitFor("text:plan notes", { state: "gone" });
  }, 30_000);

  test("the phone keeps Attach on its key bar and has no header action", async () => {
    const host = phoneHost({ brain: {}, worker_sessions: [shell], conversations: { w6: [] } });
    const app = await open(APP, { platform: "android", storage: paired, host });
    await app.host.send("links.open", { url: "mewla://sessions/w6" });
    await app.waitFor("id:terminal-attach", { timeout: 10_000 });
    expect(app.count("id:session-attach")).toBe(0);
  }, 30_000);
});
