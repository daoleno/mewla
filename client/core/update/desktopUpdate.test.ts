import { describe, expect, test } from "bun:test";
import { AUTO_CHECK_INTERVAL_MS, appUpdatePending, appUpdateRow, type AppUpdateState } from "./appUpdate";
import { DesktopUpdateController, type DesktopUpdateFound, type DesktopUpdater } from "./desktopUpdate";

class FakeUpdater implements DesktopUpdater {
  installed: string | null = "0.4.17";
  found: DesktopUpdateFound | null = null;
  checkError: Error | null = null;
  downloadError: Error | null = null;
  checks = 0;
  downloads = 0;
  restarts = 0;
  opened: string[] = [];
  time = 1_000_000;
  logged: string[] = [];
  report: Array<{ received: number; total?: number | null }> = [];
  installedVersion() {
    return this.installed;
  }
  async check() {
    this.checks += 1;
    if (this.checkError) throw this.checkError;
    return this.found;
  }
  async download(onProgress: (progress: { received: number; total?: number | null }) => void) {
    this.downloads += 1;
    for (const tick of this.report) onProgress(tick);
    if (this.downloadError) throw this.downloadError;
  }
  async installAndRestart() {
    this.restarts += 1;
  }
  async openUrl(url: string) {
    this.opened.push(url);
    return true;
  }
  now() {
    return this.time;
  }
  warn(message: string) {
    this.logged.push(message);
  }
}

const newer: DesktopUpdateFound = { version: "0.4.18", canInstall: true, url: "https://github.com/daoleno/mewla/releases/download/v0.4.18/Mewla-0.4.18-macos-aarch64.zip" };

function track(controller: DesktopUpdateController): AppUpdateState["kind"][] {
  const kinds: AppUpdateState["kind"][] = [];
  controller.subscribe((state) => kinds.push(state.kind));
  return kinds;
}

describe("Mewla for Mac's update", () => {
  test("a build with no update source shows nothing and never checks", async () => {
    const updater = new FakeUpdater();
    updater.installed = null;
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state).toEqual({ kind: "hidden" });
    expect(appUpdateRow(controller.state)).toBeNull();
    await controller.foreground();
    expect(updater.checks).toBe(0);
  });

  test("up to date: nothing waits, no dot", async () => {
    const updater = new FakeUpdater();
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state.kind).toBe("current");
    expect(appUpdatePending(controller.state)).toBe(false);
  });

  test("a version that is not newer than the installed one is not an update", async () => {
    const updater = new FakeUpdater();
    updater.found = { ...newer, version: "0.4.17" };
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state.kind).toBe("current");
  });

  test("a newer release is pending, with its version", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state.kind).toBe("available");
    expect(appUpdatePending(controller.state)).toBe(true);
    expect(appUpdateRow(controller.state)).toMatchObject({ title: "Update available", subtitle: "v0.4.18", action: "Update" });
  });

  test("one tap downloads with its progress and restarts into the new app", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.report = [{ received: 5, total: 10 }, { received: 10, total: 10 }];
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    const kinds = track(controller);
    const progress: number[] = [];
    controller.subscribe((state) => state.kind === "downloading" && progress.push(state.progress));
    await controller.press();
    expect(kinds).toEqual(["downloading", "downloading", "downloading", "ready"]);
    expect(progress).toEqual([0, 0.5, 1]);
    expect(updater.downloads).toBe(1);
    expect(updater.restarts).toBe(1);
  });

  test("a failed download says so and a tap tries again", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.downloadError = new Error("the download does not match");
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    await controller.press();
    expect(controller.state).toMatchObject({ kind: "failed", message: "the download does not match" });
    expect(appUpdateRow(controller.state)).toMatchObject({ title: "Update failed", action: "Retry" });
    expect(updater.restarts).toBe(0);
    updater.downloadError = null;
    await controller.press();
    expect(updater.restarts).toBe(1);
  });

  test("an app run from a disk image cannot replace itself: the release page opens", async () => {
    const updater = new FakeUpdater();
    updater.found = { ...newer, canInstall: false };
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    await controller.press();
    expect(updater.opened).toEqual([newer.url]);
    expect(updater.downloads).toBe(0);
    expect(controller.state.kind).toBe("available");
  });

  test("a release with no manifest yet is a failed check, logged, and a tap checks again", async () => {
    const updater = new FakeUpdater();
    updater.checkError = new Error("HTTP 404");
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state.kind).toBe("checkFailed");
    expect(updater.logged).toEqual(["[app-update] check failed"]);
    expect(appUpdatePending(controller.state)).toBe(false);
    updater.checkError = null;
    updater.time += 10_000;
    await controller.press();
    expect(updater.checks).toBe(2);
    expect(controller.state.kind).toBe("current");
  });

  test("returning to the foreground checks at most every six hours", async () => {
    const updater = new FakeUpdater();
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    await controller.foreground();
    expect(updater.checks).toBe(1);
    updater.time += AUTO_CHECK_INTERVAL_MS;
    updater.found = newer;
    await controller.foreground();
    expect(updater.checks).toBe(2);
    expect(controller.state.kind).toBe("available");
  });
});
