import { describe, expect, test } from "bun:test";
import nacl from "../vendor/nacl";
import {
  ANDROID_PACKAGE,
  AUTO_CHECK_INTERVAL_MS,
  LATEST_MANIFEST_URL,
  LATEST_SIGNATURE_URL,
  MANUAL_CHECK_COOLDOWN_MS,
  TESTFLIGHT_MANIFEST_URL,
  TESTFLIGHT_SIGNATURE_URL,
  type UpdateHostInfo,
} from "./appUpdate";
import { AppUpdateController, type AppInstaller, type DownloadProgress, type DownloadRequest } from "./controller";

const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(3));
const encode = (text: string) => new TextEncoder().encode(text);

function release(version: string) {
  return JSON.stringify({
    schema_version: 2,
    product: "mewla",
    version,
    android: { package: ANDROID_PACKAGE, version_name: version, version_code: 30100 },
    artifacts: [
      { path: `mewla-android-arm64-v${version}.apk`, role: "android_sideload_apk", sha256: "c".repeat(64), size: 1000, abi: "arm64-v8a" },
    ],
  });
}

function testflight(version: string, build: number) {
  return JSON.stringify({
    schema_version: 1,
    product: "mewla",
    platform: "ios",
    distribution: "testflight",
    bundle_identifier: "com.daoleno.mewla.preview",
    marketing_version: version,
    build_number: build,
    app_id: "6790486999",
    public_link: "https://testflight.apple.com/join/nMQheDCE",
  });
}

class FakeInstaller implements AppInstaller {
  allowed = false;
  downloads: DownloadRequest[] = [];
  installs: string[] = [];
  settingsOpened = 0;
  downloadError: Error | null = null;
  private listener: ((progress: DownloadProgress) => void) | null = null;
  private finish: (() => void) | null = null;

  download(request: DownloadRequest) {
    this.downloads.push(request);
    if (this.downloadError) return Promise.reject(this.downloadError);
    return new Promise<{ fileName: string }>((resolve) => {
      this.finish = () => resolve({ fileName: request.fileName });
    });
  }
  progress(received: number, total: number) {
    this.listener?.({ received, total });
  }
  complete() {
    this.finish?.();
  }
  onProgress(listener: (progress: DownloadProgress) => void) {
    this.listener = listener;
    return () => {
      if (this.listener === listener) this.listener = null;
    };
  }
  async canInstall() {
    return this.allowed;
  }
  async openInstallSettings() {
    this.settingsOpened += 1;
  }
  async install(fileName: string) {
    this.installs.push(fileName);
  }
}

function setup(options: { platform?: string; info?: UpdateHostInfo | null; latest?: string; installer?: FakeInstaller | null; testflight?: string } = {}) {
  let now = 1_000_000;
  const files: Record<string, Uint8Array | Error> = {};
  const publish = (manifestUrl: string, signatureUrl: string, text: string) => {
    files[manifestUrl] = encode(text);
    files[signatureUrl] = nacl.sign.detached(encode(text), keys.secretKey);
  };
  if (options.latest) publish(LATEST_MANIFEST_URL, LATEST_SIGNATURE_URL, release(options.latest));
  if (options.testflight) publish(TESTFLIGHT_MANIFEST_URL, TESTFLIGHT_SIGNATURE_URL, options.testflight);
  const fetched: string[] = [];
  const opened: string[] = [];
  const openable = new Set<string>();
  const installer = options.installer === undefined ? new FakeInstaller() : options.installer;
  const info: UpdateHostInfo | null =
    options.info === undefined ? { distribution: "github-apk", appId: ANDROID_PACKAGE, version: "0.3.0", build: "30000", arm64: true } : options.info;
  const controller = new AppUpdateController({
    platform: options.platform ?? "android",
    info: async () => info,
    installer,
    fetchBytes: async (url) => {
      fetched.push(url);
      const file = files[url];
      if (!file) throw new Error("HTTP 404");
      if (file instanceof Error) throw file;
      return file;
    },
    openUrl: async (url) => {
      opened.push(url);
      return openable.has(url);
    },
    now: () => now,
    publicKey: keys.publicKey,
  });
  return {
    controller,
    installer: installer!,
    files,
    fetched,
    opened,
    openable,
    publish,
    advance: (ms: number) => {
      now += ms;
    },
  };
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("checks", () => {
  test("start checks once and finds a newer signed release", async () => {
    const { controller, fetched } = setup({ latest: "0.3.1" });
    await controller.start();
    expect(controller.state).toMatchObject({ kind: "available", update: { version: "0.3.1", fileName: "mewla-android-arm64-v0.3.1.apk" } });
    expect(fetched.sort()).toEqual([LATEST_MANIFEST_URL, LATEST_SIGNATURE_URL].sort());
  });

  test("the same version is up to date; a failed fetch or bad signature is a failed check", async () => {
    expect((await started(setup({ latest: "0.3.0" }))).state).toEqual({ kind: "current" });
    expect((await started(setup({}))).state).toEqual({ kind: "checkFailed" });
    const forged = setup({ latest: "0.3.1" });
    forged.files[LATEST_SIGNATURE_URL] = nacl.sign.detached(encode(release("0.3.1")), nacl.sign.keyPair().secretKey);
    expect((await started(forged)).state).toEqual({ kind: "checkFailed" });
  });

  test("builds that cannot update stay hidden and never fetch", async () => {
    for (const info of [
      null,
      { distribution: "github-apk", appId: "com.daoleno.mewla.preview", version: "0.3.0", build: "30000", arm64: true },
      { distribution: "github-apk", appId: ANDROID_PACKAGE, version: "0.3.0", build: "30000", arm64: false },
    ]) {
      const s = setup({ info, latest: "0.3.1" });
      await s.controller.start();
      expect(s.controller.state).toEqual({ kind: "hidden" });
      await s.controller.press();
      await s.controller.foreground();
      expect(s.fetched).toEqual([]);
    }
  });

  test("the foreground checks at most every six hours; a tap after a three second cooldown", async () => {
    const s = setup({ latest: "0.3.0" });
    await s.controller.start();
    expect(s.fetched).toHaveLength(2);
    s.advance(AUTO_CHECK_INTERVAL_MS - 1);
    await s.controller.foreground();
    expect(s.fetched).toHaveLength(2);
    s.advance(1);
    await s.controller.foreground();
    expect(s.fetched).toHaveLength(4);
    s.advance(MANUAL_CHECK_COOLDOWN_MS - 1);
    await s.controller.press();
    expect(s.fetched).toHaveLength(4);
    s.advance(1);
    await s.controller.press();
    expect(s.fetched).toHaveLength(6);
  });

  test("a failed check is retried by a tap", async () => {
    const s = setup({});
    await s.controller.start();
    expect(s.controller.state.kind).toBe("checkFailed");
    s.publish(LATEST_MANIFEST_URL, LATEST_SIGNATURE_URL, release("0.3.2"));
    s.advance(MANUAL_CHECK_COOLDOWN_MS);
    await s.controller.press();
    expect(s.controller.state).toMatchObject({ kind: "available", update: { version: "0.3.2" } });
  });
});

describe("download and install", () => {
  test("downloads with progress, then hands the verified file to the installer", async () => {
    const s = setup({ latest: "0.3.1" });
    s.installer.allowed = true;
    await s.controller.start();
    const pressed = s.controller.press();
    expect(s.controller.state).toMatchObject({ kind: "downloading", progress: 0 });
    expect(s.installer.downloads).toEqual([
      {
        url: "https://github.com/daoleno/mewla/releases/download/v0.3.1/mewla-android-arm64-v0.3.1.apk",
        fileName: "mewla-android-arm64-v0.3.1.apk",
        sha256: "c".repeat(64),
        size: 1000,
      },
    ]);
    s.installer.progress(426, 1000);
    expect(s.controller.state).toMatchObject({ kind: "downloading", progress: 0.426 });
    s.installer.progress(5000, 1000);
    expect(s.controller.state).toMatchObject({ kind: "downloading", progress: 1 });
    // A second tap while it downloads starts nothing.
    await s.controller.press();
    expect(s.installer.downloads).toHaveLength(1);
    s.installer.complete();
    await pressed;
    expect(s.controller.state).toMatchObject({ kind: "ready", fileName: "mewla-android-arm64-v0.3.1.apk" });
    expect(s.installer.installs).toEqual(["mewla-android-arm64-v0.3.1.apk"]);
    // The installer was dismissed: Install opens it again.
    await s.controller.press();
    expect(s.installer.installs).toHaveLength(2);
  });

  test("asks for app installs, then installs once the person allowed it and came back", async () => {
    const s = setup({ latest: "0.3.1" });
    await s.controller.start();
    const pressed = s.controller.press();
    s.installer.complete();
    await pressed;
    expect(s.controller.state).toMatchObject({ kind: "needsPermission", fileName: "mewla-android-arm64-v0.3.1.apk" });
    expect(s.installer.settingsOpened).toBe(1);
    // Back without allowing: it waits, and does not check.
    await s.controller.foreground();
    expect(s.controller.state.kind).toBe("needsPermission");
    expect(s.installer.installs).toEqual([]);
    // Continue opens the settings again.
    await s.controller.press();
    expect(s.installer.settingsOpened).toBe(2);
    s.installer.allowed = true;
    await s.controller.foreground();
    expect(s.controller.state.kind).toBe("ready");
    expect(s.installer.installs).toEqual(["mewla-android-arm64-v0.3.1.apk"]);
  });

  test("a failed download says so and Retry downloads again", async () => {
    const s = setup({ latest: "0.3.1" });
    await s.controller.start();
    s.installer.downloadError = new Error("Download doesn't match the signed release.");
    await s.controller.press();
    expect(s.controller.state).toMatchObject({ kind: "failed", message: "Download doesn't match the signed release." });
    s.installer.downloadError = null;
    s.installer.allowed = true;
    const retried = s.controller.press();
    expect(s.controller.state.kind).toBe("downloading");
    s.installer.complete();
    await retried;
    expect(s.controller.state.kind).toBe("ready");
    expect(s.installer.downloads).toHaveLength(2);
  });

  test("progress after the download ended changes nothing", async () => {
    const s = setup({ latest: "0.3.1" });
    s.installer.allowed = true;
    await s.controller.start();
    const pressed = s.controller.press();
    s.installer.complete();
    await pressed;
    s.installer.progress(10, 1000);
    await tick();
    expect(s.controller.state.kind).toBe("ready");
  });
});

describe("TestFlight", () => {
  const info = { distribution: "testflight", appId: "com.daoleno.mewla.preview", version: "0.3.0", build: "140" };

  test("a newer TestFlight build is announced; Update opens TestFlight's app page, else the invite link", async () => {
    const s = setup({ platform: "ios", info, installer: null, testflight: testflight("0.3.0", 141) });
    await s.controller.start();
    expect(s.controller.state).toMatchObject({ kind: "testflightAvailable", update: { version: "0.3.0", buildNumber: 141 } });
    s.openable.add("https://testflight.apple.com/join/nMQheDCE");
    await s.controller.press();
    expect(s.opened).toEqual(["itms-beta://beta.itunes.apple.com/v1/app/6790486999", "https://testflight.apple.com/join/nMQheDCE"]);
    s.openable.add("itms-beta://beta.itunes.apple.com/v1/app/6790486999");
    await s.controller.press();
    expect(s.opened).toHaveLength(3);
  });

  test("the running build, or a manifest for another app, is no update", async () => {
    expect((await started(setup({ platform: "ios", info, installer: null, testflight: testflight("0.3.0", 140) }))).state).toEqual({ kind: "current" });
    const other = setup({ platform: "ios", info: { ...info, appId: "com.daoleno.mewla" }, installer: null, testflight: testflight("0.3.1", 1) });
    expect((await started(other)).state).toEqual({ kind: "checkFailed" });
  });

  test("App Store and local builds show nothing", async () => {
    for (const distribution of ["app-store", "none"]) {
      const s = setup({ platform: "ios", info: { ...info, distribution }, installer: null, testflight: testflight("0.4.0", 1) });
      await s.controller.start();
      expect(s.controller.state).toEqual({ kind: "hidden" });
      expect(s.fetched).toEqual([]);
    }
  });
});

async function started(s: ReturnType<typeof setup>) {
  await s.controller.start();
  return s.controller;
}
