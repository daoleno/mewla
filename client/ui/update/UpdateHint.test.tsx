import { describe, expect, open, test } from "nsh/test";
import { expoTokenService, paired, phoneHost } from "../testing/fixtures";

const APP = "../FakeDaemonApp.tsx";
const json = (body: unknown) => ({ status: 200, headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
const DAEMON_UPDATE = "GET http://127.0.0.1:1/daemon-update";
const apk = { version: "0.4.4", versionCode: 40400, fileName: "mewla-android-arm64-v0.4.4.apk", url: "https://github.com/daoleno/mewla/releases/download/v0.4.4/mewla-android-arm64-v0.4.4.apk", sha256: "c".repeat(64), size: 1000 };

/** The app on Desk; `appUpdate` is this app's update state, `computer` what Desk says of its Mewla. */
function setup(options: { appUpdate?: unknown; computer?: unknown; storage?: Record<string, string> } = {}) {
  const h = phoneHost({ brain: {} }, { "app.appUpdatePressed": () => undefined });
  return {
    platform: "android" as const,
    storage: { ...paired, ...options.storage },
    network: { ...expoTokenService, ...(options.computer ? { [DAEMON_UPDATE]: json(options.computer) } : {}) },
    host: { ...h, params: { ...h.params, ...(options.appUpdate ? { appUpdate: options.appUpdate } : {}) } },
  };
}

describe("the light update hint", () => {
  test("no update: no ink dot on the cat, none on the computer page's Settings row", async () => {
    const app = await open(APP, setup({ computer: { version: "0.4.4", supported: true } }));
    await app.waitFor("id:home-cat", { timeout: 10_000 });
    await app.wait(500);
    expect(app.shows("id:home-cat-update")).toBe(false);
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    expect(app.shows("id:computer-settings-update")).toBe(false);
    expect(app.shows("text:Update available")).toBe(false);
  }, 20_000);

  test("this app's update: the cat and Settings' row carry the dot; Settings puts its row first; seen, the cat's goes and Settings' stays", async () => {
    const app = await open(APP, setup({ appUpdate: { kind: "available", update: apk } }));
    await app.waitFor("id:home-cat-update", { timeout: 10_000 });
    expect(app.shows("id:home-cat-dot")).toBe(false);
    // Cat → the computer page: its Settings row carries the same dot (no words).
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings-update");
    // → Settings: the update row first, with the version and Update.
    await app.press("id:computer-settings");
    await app.waitFor("id:settings-update");
    expect(app.shows("text:Update available")).toBe(true);
    expect(app.shows("text:v0.4.4")).toBe(true);
    expect(app.count("id:app-update")).toBe(1);
    expect(app.texts().indexOf("Update available")).toBeLessThan(app.texts().indexOf("Channels"));
    await app.press("id:app-update-action");
    await app.waitUntil(() => app.host.posts().some((post) => post.name === "app.appUpdatePressed"));
    // Back on the computer page the row keeps its dot until the update is in; home's cat lets it go.
    await app.press("id:page-back");
    await app.waitFor("id:computer-settings-update");
    await app.press("id:page-back");
    await app.waitFor("id:home-cat");
    await app.waitUntil(() => !app.shows("id:home-cat-update"));
    expect(JSON.parse(app.storage()["mewla:update_seen"])).toEqual({ app: "0.4.4" });
    // Installed (the new build is current): the row's dot goes too, and Settings' row is About's again.
    await app.host.send("app.appUpdate", { kind: "current" });
    await app.press("id:home-cat");
    await app.waitFor("id:computer-settings");
    await app.waitUntil(() => !app.shows("id:computer-settings-update"));
  }, 30_000);

  test("an app update seen before stays off the cat after a restart; the next version lights it again", async () => {
    const seen = { "mewla:update_seen": JSON.stringify({ app: "0.4.4" }) };
    const before = await open(APP, setup({ appUpdate: { kind: "available", update: apk }, storage: seen }));
    await before.waitFor("id:home-cat", { timeout: 10_000 });
    await before.wait(500);
    expect(before.shows("id:home-cat-update")).toBe(false);
    const next = await open(APP, setup({ appUpdate: { kind: "available", update: { ...apk, version: "0.4.5" } }, storage: seen }));
    await next.waitFor("id:home-cat-update", { timeout: 10_000 });
  }, 30_000);

  test("the computer's update: the cat carries the dot, its page has the update row; seen there, the cat's dot goes; Settings' row has none", async () => {
    const app = await open(APP, setup({ computer: { version: "0.4.3", supported: true, latest: "0.4.4" } }));
    await app.waitFor("id:home-cat-update", { timeout: 10_000 });
    await app.press("id:home-cat");
    await app.waitFor("text:Update available · 0.4.4");
    expect(app.shows("id:fix-update-optional-button")).toBe(true);
    expect(app.shows("id:computer-settings-update")).toBe(false);
    await app.press("id:page-back");
    await app.waitFor("id:home-cat");
    await app.waitUntil(() => !app.shows("id:home-cat-update"));
    expect(JSON.parse(app.storage()["mewla:update_seen"])).toEqual({ computers: { s1: "0.4.4" } });
  }, 30_000);

  test("a required update is the red dot alone: red wins over ink", async () => {
    const app = await open(APP, setup({ appUpdate: { kind: "available", update: apk }, computer: { version: "0.0.1", supported: true, latest: "0.4.4" } }));
    await app.waitFor("id:home-cat-dot", { timeout: 10_000 });
    expect(app.shows("id:home-cat-update")).toBe(false);
  }, 20_000);
});
