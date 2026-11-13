import { describe, expect, open, test } from "nsh/test";
import { paired, phoneHost } from "../testing/fixtures";
import { IDS, storage } from "../fixture/scenario";

// Trial: three pages (SPEC 1, under trial since 11-13). Brain is the middle page; the computer page is to
// its left, the running things to its right.
async function home() {
  const app = await open("../fixture/FixtureApp.tsx", { platform: "android", storage, host: phoneHost({}) });
  await app.waitFor("id:composer", { timeout: 10_000 });
  return app;
}

describe("Trial: three pages", () => {
  test("the cat block shows the computer page; Back returns to Brain with the draft as it was", async () => {
    const app = await home();
    await app.type("id:composer", "half a thought");
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    expect(app.shows("id:composer")).toBe(false);
    expect(app.count("id:back")).toBe(0);
    await app.back();
    await app.waitFor("id:composer");
    expect(app.value("id:composer")).toBe("half a thought");
  }, 30_000);

  test("the state control shows the running things as a page; Back returns to Brain", async () => {
    const app = await home();
    await app.waitFor("text:3 need you", { timeout: 10_000 });
    await app.press("id:state-control");
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    expect(app.shows("id:home-page-running")).toBe(true);
    expect(app.shows("id:composer")).toBe(false);
    await app.back();
    await app.waitFor("id:composer");
    expect(app.shows("text:Cache layer for the catalogue API")).toBe(false);
  }, 30_000);

  test("a Session opened from the running page pushes, and Back returns to that page", async () => {
    const app = await home();
    await app.waitFor("text:3 need you", { timeout: 10_000 });
    await app.press("id:state-control");
    await app.waitFor("text:Flaky checkout test", { timeout: 10_000 });
    await app.press("text:Flaky checkout test");
    await app.waitFor("id:session-menu", { timeout: 10_000 });
    await app.back();
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    expect(app.shows("id:home-page-running")).toBe(true);
    expect(app.shows("id:composer")).toBe(false);
  }, 30_000);

  test("on the computer page, a link to Brain shows Brain", async () => {
    const app = await home();
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    await app.host.send("links.open", { url: "mewla://brain" });
    await app.waitFor("id:composer");
    expect(app.shows("id:computer-settings")).toBe(false);
  }, 30_000);

  test("a swipe left past half a page shows the running things; a swipe right from there returns to Brain", async () => {
    const app = await home();
    await app.drag("id:home-page-brain", { x: -260 });
    await app.waitFor("text:Cache layer for the catalogue API", { timeout: 10_000 });
    await app.drag("id:home-page-running", { x: 260 });
    await app.waitFor("id:composer");
    await app.drag("id:home-page-brain", { x: 260 });
    await app.waitFor("id:computer-settings");
  }, 30_000);

  test("a code block in Brain's thread wraps its long lines, with Copy; one in a Session keeps its sideways scroll", async () => {
    const host_worker = { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: 1_762_840_800_000, process_id: 4242 };
    const code = "Run:\n\n```sh\n" + "mewla update --channel beta ".repeat(8) + "\n```\n";
    const reply = { id: "a1", seq: 1, timestamp: "2026-11-12T07:00:01.000Z", kind: "assistant_message", role: "assistant", body: code };
    const brain = await open("../FakeDaemonApp.tsx", { platform: "android", storage: paired, host: phoneHost({ brain: { chat_thread_id: "t-now", host_worker }, conversations: { "brain-host": [reply] } }) });
    await brain.waitFor("id:code-block", { timeout: 10_000 });
    expect(brain.count("id:code-wrapped")).toBe(1);
    await brain.wait(300);
    await brain.press("label:Copy");
    await brain.waitUntil(() => (brain.clipboard() ?? "").includes("mewla update --channel beta"));

    const app = await home();
    await app.host.send("links.open", { url: `mewla://sessions/${IDS.ratelimit}` });
    await app.waitFor("id:code-block", { timeout: 10_000 });
    expect(app.count("id:code-wrapped")).toBe(0);
  }, 40_000);
});
