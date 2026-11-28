import { describe, expect, open, test } from "nsh/test";
import { desktopArtifact } from "../../core/update/desktopUpdate";
import { paired, phoneHost } from "../testing/fixtures";

// Mewla for Mac's Update button: in the title bar once the newer app has downloaded, one press posts the update.
const APP = "../FakeDaemonApp.tsx";
const update = desktopArtifact("0.4.18");
const brain = { chat_thread_id: "t-now", host_worker: { id: "brain-host", name: "Brain", cwd: "/home/me", command: "claude", started_at: "2026-11-11T07:00:00.000Z" } };
const pairing = { expires_at: "2099-01-01T00:00:00Z", daemon_id: "a".repeat(64), daemon_public_key: "b".repeat(64), phone_address: "http://192.168.1.20:9876" };

function setup(appUpdate: unknown, extra: { platform?: "macos" | "android"; width?: number } = {}) {
  const base = phoneHost({ brain, conversations: { "brain-host": [] } }, { "app.pairing": () => pairing, "app.appUpdatePressed": () => undefined });
  return { platform: extra.platform ?? ("macos" as const), width: extra.width ?? 1280, height: 800, storage: paired, host: { ...base, params: { ...base.params, appUpdate } } };
}

describe("the Update button", () => {
  test("nothing while the update is checked, downloading or absent: the cat carries no dot either", async () => {
    for (const state of [{ kind: "current" }, { kind: "checking" }, { kind: "downloading", update, progress: 0.4 }, { kind: "available", update }]) {
      const app = await open(APP, setup(state));
      await app.waitFor("id:home-cat", { timeout: 15_000 });
      await app.wait(300);
      expect(app.shows("id:update-button")).toBe(false);
      expect(app.shows("id:home-cat-update")).toBe(false);
    }
  }, 60_000);

  test("it appears at the top of the window when the download is done, and one press goes to the update", async () => {
    const app = await open(APP, setup({ kind: "downloading", update, progress: 0.9 }));
    await app.waitFor("id:home-cat", { timeout: 15_000 });
    expect(app.shows("id:update-button")).toBe(false);
    await app.host.send("app.appUpdate", { kind: "ready", update, fileName: "" });
    // The headless window has no title bar row (the real one is in the Mac proof): the pane header's own place.
    await app.waitFor("id:left-pane >> id:update-button", { timeout: 5_000 });
    expect(app.shows("id:left-pane >> text:Update")).toBe(true);
    await app.press("id:update-button");
    await app.waitUntil(() => app.host.posts().some((post) => post.name === "app.appUpdatePressed"), { timeout: 5_000 });
    // Installed (the new app is current): it is gone.
    await app.host.send("app.appUpdate", { kind: "current" });
    await app.waitUntil(() => !app.shows("id:update-button"));
  }, 30_000);

  test("one column (the window under 1024): Home's header has it, and so has a page pushed over Home", async () => {
    const app = await open(APP, setup({ kind: "ready", update, fileName: "" }, { width: 800 }));
    await app.waitFor("id:home-header >> id:update-button", { timeout: 15_000 });
    await app.press("id:home-cat");
    await app.waitFor("id:page-header >> id:update-button", { timeout: 10_000 });
  }, 30_000);

  test("a phone's ready update (the APK) has no such button", async () => {
    const app = await open(APP, setup({ kind: "ready", update, fileName: "x.apk" }, { platform: "android", width: 390 }));
    await app.waitFor("id:home-cat", { timeout: 15_000 });
    await app.wait(300);
    expect(app.shows("id:update-button")).toBe(false);
  }, 30_000);
});
