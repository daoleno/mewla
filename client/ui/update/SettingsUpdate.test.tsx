import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
const DAEMON_UPDATE = "GET http://127.0.0.1:1/daemon-update";
const apk = { version: "0.4.4", versionCode: 40400, fileName: "mewla-android-arm64-v0.4.4.apk", url: "https://github.com/daoleno/mewla/releases/download/v0.4.4/mewla-android-arm64-v0.4.4.apk", sha256: "c".repeat(64), size: 1000 };

/** The app on Desk; `appUpdate` is this app's update state, `computer` what Desk says of its Mewla. */
function setup(options: { appUpdate?: unknown; computer?: unknown } = {}) {
  const h = phoneHost({ brain: {} }, { "app.appUpdatePressed": () => undefined });
  return {
    platform: "android" as const,
    storage: paired,
    network: { ...expoTokenService, ...(options.computer ? { [DAEMON_UPDATE]: json(options.computer) } : {}) },
    host: { ...h, params: { ...h.params, ...(options.appUpdate ? { appUpdate: options.appUpdate } : {}) } },
  };
}

// Home's cat carries only the red "needs you" dot: an update is found in Settings, where its row is first.
describe("where a phone finds an update", () => {
  test("this app's update: the computer page's Settings row carries a dot, Settings puts its row first, and home's cat has none", async () => {
    const app = await open(APP, setup({ appUpdate: { kind: "available", update: apk } }));
    await app.waitFor("id:home-cat", { timeout: 10_000 });
    await app.wait(300);
    expect(app.shows("id:home-cat-update")).toBe(false);
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings-update");
    await app.press("id:computer-settings");
    await app.waitFor("id:settings-update");
    expect(app.shows("text:Update available")).toBe(true);
    expect(app.shows("text:v0.4.4")).toBe(true);
    expect(app.count("id:app-update")).toBe(1);
    expect(app.texts().indexOf("Update available")).toBeLessThan(app.texts().indexOf("Agents"));
    await app.press("id:app-update-action");
    await app.waitUntil(() => app.host.posts().some((post) => post.name === "app.appUpdatePressed"));
    // Installed (the new build is current): the row's dot goes, and Settings' row is About's again.
    await app.host.send("app.appUpdate", { kind: "current" });
    await app.press("id:page-back");
    await app.waitFor("id:computer-settings");
    await app.waitUntil(() => !app.shows("id:computer-settings-update"));
  }, 30_000);

  test("an update found after the page is open lights Settings' row, and a newer one lights it again", async () => {
    const app = await open(APP, setup({ appUpdate: { kind: "current" } }));
    await app.waitFor("id:home-cat", { timeout: 10_000 });
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    expect(app.shows("id:computer-settings-update")).toBe(false);
    await app.host.send("app.appUpdate", { kind: "available", update: apk });
    await app.waitFor("id:computer-settings-update");
    await app.host.send("app.appUpdate", { kind: "current" });
    await app.waitUntil(() => !app.shows("id:computer-settings-update"));
    await app.host.send("app.appUpdate", { kind: "available", update: { ...apk, version: "0.4.5" } });
    await app.waitFor("id:computer-settings-update");
  }, 30_000);

  test("the computer's update: its page has the update row; the cat has no dot and Settings' row none", async () => {
    const app = await open(APP, setup({ computer: { version: "0.4.3", supported: true, latest: "0.4.4" } }));
    await app.waitFor("id:home-cat", { timeout: 10_000 });
    await app.wait(500);
    expect(app.shows("id:home-cat-update")).toBe(false);
    await app.press("id:home-cat");
    await app.waitFor("text:Update available · 0.4.4");
    expect(app.shows("id:fix-update-optional-button")).toBe(true);
    expect(app.shows("id:computer-settings-update")).toBe(false);
  }, 30_000);

  test("a required update is the red dot alone", async () => {
    const app = await open(APP, setup({ appUpdate: { kind: "available", update: apk }, computer: { version: "0.0.1", supported: true, latest: "0.4.4" } }));
    await app.waitFor("id:home-cat-dot", { timeout: 10_000 });
    expect(app.shows("id:home-cat-update")).toBe(false);
  }, 20_000);
});
