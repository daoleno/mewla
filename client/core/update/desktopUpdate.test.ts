import { describe, expect, test } from "bun:test";
import { AUTO_CHECK_INTERVAL_MS, appUpdatePending, appUpdateRow, type AppUpdateState } from "./appUpdate";
import { DesktopUpdateController, desktopArtifact, desktopUpdateButton, isNoMacFiles, type DesktopUpdateFound, type DesktopUpdater } from "./desktopUpdate";

class FakeUpdater implements DesktopUpdater {
  installed: string | null = "0.4.17";
  found: DesktopUpdateFound | null = null;
  checkError: Error | null = null;
  downloadError: Error | null = null;
  stageError: Error | null = null;
  restartError: Error | null = null;
  checks = 0;
  downloads = 0;
  staged = 0;
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
  async installOnQuit() {
    if (this.stageError) throw this.stageError;
    this.staged += 1;
  }
  async installAndRestart() {
    this.restarts += 1;
    if (this.restartError) throw this.restartError;
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

  test("a newer release downloads on its own, waits for the app's quit and shows the Update button: nobody pressed anything", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.report = [{ received: 5, total: 10 }, { received: 10, total: 10 }];
    const controller = new DesktopUpdateController(updater);
    const kinds = track(controller);
    const progress: number[] = [];
    controller.subscribe((state) => state.kind === "downloading" && progress.push(state.progress));
    await controller.start();
    expect(kinds).toEqual(["idle", "checking", "downloading", "downloading", "downloading", "ready"]);
    expect(progress).toEqual([0, 0.5, 1]);
    expect(updater.downloads).toBe(1);
    expect(updater.staged).toBe(1);
    expect(updater.restarts).toBe(0);
    expect(controller.state).toMatchObject({ kind: "ready", update: { version: "0.4.18" } });
    expect(appUpdatePending(controller.state)).toBe(true);
    expect(desktopUpdateButton(controller.state)).toBe(true);
    expect(appUpdateRow(controller.state)).toMatchObject({ title: "Ready to install", subtitle: "v0.4.18" });
  });

  test("the button shows only while the update waits downloaded (or its install failed), not while checking or downloading", () => {
    const update = desktopArtifact("0.4.18");
    for (const state of [{ kind: "hidden" }, { kind: "idle" }, { kind: "checking" }, { kind: "current" }, { kind: "checkFailed" }, { kind: "available", update }, { kind: "downloading", update, progress: 0.3 }] as const) expect(desktopUpdateButton(state)).toBe(false);
    expect(desktopUpdateButton({ kind: "ready", update, fileName: "" })).toBe(true);
    expect(desktopUpdateButton({ kind: "failed", update, message: "x" })).toBe(true);
  });

  test("one click on the button restarts into the new app, with nothing downloaded again", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    await controller.press();
    expect(updater.restarts).toBe(1);
    expect(updater.downloads).toBe(1);
    expect(updater.staged).toBe(1);
  });

  test("a second click while it restarts does nothing more", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    await Promise.all([controller.press(), controller.press()]);
    expect(updater.restarts).toBe(1);
  });

  test("a failed download says so, shows the button, and a click downloads again and restarts", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.downloadError = new Error("the download does not match");
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state).toMatchObject({ kind: "failed", message: "the download does not match" });
    expect(desktopUpdateButton(controller.state)).toBe(true);
    expect(appUpdateRow(controller.state)).toMatchObject({ title: "Update failed", action: "Retry" });
    expect(updater.restarts).toBe(0);
    updater.downloadError = null;
    await controller.press();
    expect(updater.downloads).toBe(2);
    expect(updater.restarts).toBe(1);
  });

  test("an update that cannot be staged for the quit fails too, and is tried again", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.stageError = new Error("refused");
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state).toMatchObject({ kind: "failed", message: "refused" });
    updater.stageError = null;
    updater.time += AUTO_CHECK_INTERVAL_MS;
    await controller.foreground();
    expect(controller.state.kind).toBe("ready");
    expect(updater.staged).toBe(1);
  });

  test("a failed download is tried again by itself at the next check's time, not before", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.downloadError = new Error("offline");
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    updater.downloadError = null;
    updater.time += AUTO_CHECK_INTERVAL_MS - 1;
    await controller.foreground();
    expect(controller.state.kind).toBe("failed");
    updater.time += 1;
    await controller.foreground();
    expect(controller.state.kind).toBe("ready");
    expect(updater.downloads).toBe(2);
    expect(updater.restarts).toBe(0);
  });

  test("a restart that fails keeps the update staged for the quit: a click tries the restart again, no download", async () => {
    const updater = new FakeUpdater();
    updater.found = newer;
    updater.restartError = new Error("the app cannot be replaced");
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    await controller.press();
    expect(controller.state).toMatchObject({ kind: "failed", message: "the app cannot be replaced" });
    expect(desktopUpdateButton(controller.state)).toBe(true);
    updater.restartError = null;
    await controller.press();
    expect(updater.restarts).toBe(2);
    expect(updater.downloads).toBe(1);
    // Nothing downloads again by itself either: it waits for the quit.
    updater.time += AUTO_CHECK_INTERVAL_MS;
    await controller.foreground();
    expect(updater.downloads).toBe(1);
  });

  test("an app run from a disk image cannot replace itself: nothing downloads, the release page opens, no button", async () => {
    const updater = new FakeUpdater();
    updater.found = { ...newer, canInstall: false };
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state.kind).toBe("available");
    expect(desktopUpdateButton(controller.state)).toBe(false);
    await controller.press();
    expect(updater.opened).toEqual([newer.url]);
    expect(updater.downloads).toBe(0);
    expect(controller.state.kind).toBe("available");
  });

  // NSH rejects a manifest the release does not have as "<url>: HTTP 404".
  const NO_MANIFEST = "https://github.com/daoleno/mewla/releases/latest/download/latest.json: HTTP 404";

  test("a latest release without Mac files is no update: up to date, nothing logged, no dot", async () => {
    const updater = new FakeUpdater();
    updater.checkError = new Error(NO_MANIFEST);
    const controller = new DesktopUpdateController(updater);
    await controller.start();
    expect(controller.state.kind).toBe("current");
    expect(updater.logged).toEqual([]);
    expect(appUpdatePending(controller.state)).toBe(false);
    expect(appUpdateRow(controller.state)).toMatchObject({ title: "Check for updates", value: "Up to date" });
  });

  test("only a missing manifest is no update: a missing signature, a bad manifest and no network stay failed checks, logged, and a tap checks again", async () => {
    for (const message of ["https://github.com/daoleno/mewla/releases/latest/download/latest.json.sig: HTTP 404", "https://x/latest.json: HTTP 500", "the manifest does not verify", "offline"]) {
      const updater = new FakeUpdater();
      updater.checkError = new Error(message);
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
    }
  });

  test("isNoMacFiles reads NSH's rejection", () => {
    expect(isNoMacFiles(new Error(NO_MANIFEST))).toBe(true);
    expect(isNoMacFiles(NO_MANIFEST)).toBe(true);
    expect(isNoMacFiles(new Error("HTTP 404"))).toBe(false);
    expect(isNoMacFiles(new Error("https://x/latest.json.sig: HTTP 404"))).toBe(false);
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
    expect(controller.state.kind).toBe("ready");
  });
});
