import { describe, expect, open, test } from "nsh/test";
import nacl from "../../core/vendor/nacl";

const HARNESS = "./testing/Harness.tsx";
const ACTION = "id:app-update-action";
const MANIFEST = "https://github.com/daoleno/mewla/releases/latest/download/release-manifest.json";
const TESTFLIGHT = "https://github.com/daoleno/mewla/releases/download/ios-testflight/testflight-manifest.json";
const APK = "mewla-android-arm64-v0.3.1.apk";
const keys = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(3));

const B64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
function base64(bytes: Uint8Array): string {
  let out = "";
  for (let index = 0; index < bytes.length; index += 3) {
    const n = (bytes[index] << 16) | ((bytes[index + 1] ?? 0) << 8) | (bytes[index + 2] ?? 0);
    out += B64[(n >> 18) & 63] + B64[(n >> 12) & 63];
    out += index + 1 < bytes.length ? B64[(n >> 6) & 63] : "=";
    out += index + 2 < bytes.length ? B64[n & 63] : "=";
  }
  return out;
}

/** The release server: a signed manifest per URL, or nothing (a failed check). */
function server(files: Record<string, string>) {
  const served: Record<string, string> = {};
  for (const [url, text] of Object.entries(files)) {
    const data = new TextEncoder().encode(text);
    served[url] = base64(data);
    served[`${url}.sig`] = base64(nacl.sign.detached(data, keys.secretKey));
  }
  return (data: unknown) => {
    const url = (data as { url: string }).url;
    if (!served[url]) throw new Error("HTTP 404");
    return served[url];
  };
}

function release(version: string) {
  return JSON.stringify({
    schema_version: 2,
    product: "mewla",
    version,
    android: { package: "com.daoleno.mewla", version_name: version, version_code: 30100 },
    artifacts: [{ path: `mewla-android-arm64-v${version}.apk`, role: "android_sideload_apk", sha256: "c".repeat(64), size: 1000, abi: "arm64-v8a" }],
  });
}

const apk = { distribution: "github-apk", appId: "com.daoleno.mewla", version: "0.3.0", build: "30000", arm64: true };
const artifact = { version: "0.3.1", versionCode: 30100, fileName: APK, url: `https://github.com/daoleno/mewla/releases/download/v0.3.1/${APK}`, sha256: "c".repeat(64), size: 1000 };

/**
 * The GitHub APK's host. A step waits until the app is quiet, which an open host call
 * is not, so each download answers at once: `downloads` says how, in order (true: the
 * verified file; false: it failed). Installs are allowed while `allowed`.
 */
function android(latest: string | null, options: { allowed?: boolean; info?: unknown; downloads?: boolean[] } = {}) {
  const state = { allowed: options.allowed ?? true, downloads: [...(options.downloads ?? [true])] };
  return {
    state,
    options: {
      platform: "android" as const,
      host: {
        handlers: {
          "update.info": options.info ?? apk,
          "test.fetch": server(latest ? { [MANIFEST]: release(latest) } : {}),
          "update.download": (data: unknown) => {
            if (state.downloads.shift() === false) throw new Error("Download stopped early.");
            return { fileName: (data as { fileName: string }).fileName };
          },
          "update.canInstall": () => state.allowed,
          "update.openInstallSettings": null,
          "update.install": null,
        },
      },
    },
  };
}

const called = (app: { host: { calls(): { name: string; data: unknown }[] } }, name: string) => app.host.calls().filter((c) => c.name === name);

describe("About update row on the GitHub APK", () => {
  test("checks at start: up to date, and a tap checks again", async () => {
    const { options } = android("0.3.0");
    const app = await open(HARNESS, options);
    await app.waitFor("text:Up to date");
    expect(app.texts()).toContain("Check for updates");
    expect(app.texts()).not.toContain("Update pending");
    expect(called(app, "test.fetch")).toHaveLength(2);
  });

  test("a failed check offers Retry", async () => {
    const { options } = android(null);
    const app = await open(HARNESS, options);
    await app.waitFor("text:Retry");
    expect(app.texts()).toContain("Check for updates");
  });

  test("Update downloads the signed APK and opens the installer; Install opens it again", async () => {
    const { options } = android("0.3.1");
    const app = await open(HARNESS, options);
    await app.waitFor("text:Update available");
    expect(app.texts()).toContain("v0.3.1");
    expect(app.texts()).toContain("Update pending");
    await app.press(ACTION);
    expect(called(app, "update.download").map((c) => c.data)).toEqual([
      { url: artifact.url, fileName: APK, sha256: "c".repeat(64), size: 1000 },
    ]);
    await app.waitFor("text:Ready to install");
    expect(app.texts()).toContain("v0.3.1");
    expect(app.texts()).toContain("Install");
    expect(called(app, "update.install").map((c) => c.data)).toEqual([{ fileName: APK }]);
    await app.press(ACTION);
    expect(called(app, "update.install")).toHaveLength(2);
  });

  test("Allow app installs: Continue opens the settings; back allowed, it installs", async () => {
    const { options, state } = android("0.3.1", { allowed: false });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Update available");
    await app.press(ACTION);
    await app.waitFor("text:Allow app installs");
    expect(app.texts()).toContain("Continue");
    expect(called(app, "update.openInstallSettings")).toHaveLength(1);
    await app.press(ACTION);
    expect(called(app, "update.openInstallSettings")).toHaveLength(2);
    // Back without allowing: it keeps waiting.
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    expect(app.texts()).toContain("Allow app installs");
    state.allowed = true;
    await app.setLifecycle("background");
    await app.setLifecycle("active");
    await app.waitFor("text:Ready to install");
    expect(called(app, "update.install")).toHaveLength(1);
  });

  test("a failed download says Update failed; Retry downloads again", async () => {
    const { options } = android("0.3.1", { downloads: [false, true] });
    const app = await open(HARNESS, options);
    await app.waitFor("text:Update available");
    await app.press(ACTION);
    await app.waitFor("text:Update failed");
    expect(app.texts()).toContain("Update pending");
    expect(app.texts()).toContain("Retry");
    await app.press(ACTION);
    await app.waitFor("text:Ready to install");
    expect(called(app, "update.download")).toHaveLength(2);
  });

  test("no row where the build cannot update itself", async () => {
    for (const info of [{ ...apk, arm64: false }, { ...apk, appId: "com.daoleno.mewla.preview" }]) {
      const { options } = android("0.3.1", { info });
      const app = await open(HARNESS, options);
      await app.wait(50);
      expect(app.count("id:app-update")).toBe(0);
      expect(called(app, "test.fetch")).toHaveLength(0);
    }
  });
});

describe("About update row states", () => {
  const shows = async (state: unknown) => {
    const app = await open(HARNESS, { platform: "android", host: { params: { state }, handlers: { "test.pressed": null } } });
    return app;
  };

  test("each state in a few words and its one button", async () => {
    const rows: [unknown, string[], string | null][] = [
      [{ kind: "checking" }, ["Check for updates"], null],
      [{ kind: "current" }, ["Check for updates", "Up to date"], null],
      [{ kind: "checkFailed" }, ["Check for updates"], "Retry"],
      [{ kind: "available", update: artifact }, ["Update available", "v0.3.1"], "Update"],
      [{ kind: "downloading", update: artifact, progress: 0.426 }, ["Downloading", "42%"], null],
      [{ kind: "needsPermission", update: artifact, fileName: APK }, ["Allow app installs"], "Continue"],
      [{ kind: "ready", update: artifact, fileName: APK }, ["Ready to install", "v0.3.1"], "Install"],
      [{ kind: "failed", update: artifact, message: "x" }, ["Update failed"], "Retry"],
    ];
    for (const [state, texts, action] of rows) {
      const app = await shows(state);
      for (const text of texts) expect(app.texts()).toContain(text);
      expect(app.count(ACTION)).toBe(action ? 1 : 0);
      if (action) {
        expect(app.texts()).toContain(action);
        await app.press(ACTION);
        expect(app.host.posts()).toHaveLength(1);
      }
      expect(app.texts().includes("Update pending")).toBe(typeof state === "object" && state !== null && "update" in state);
    }
  });

  test("Up to date checks again on a tap; busy and downloading rows don't take one", async () => {
    const current = await shows({ kind: "current" });
    await current.press("id:app-update");
    expect(current.host.posts()).toHaveLength(1);
    const downloading = await shows({ kind: "downloading", update: artifact, progress: 0.5 });
    // Nothing there hears a press: the step fails or does nothing.
    await downloading.press("id:app-update").catch(() => {});
    expect(downloading.host.posts()).toHaveLength(0);
  });

  test("a download's ticks move its percentage and meter in the same row", async () => {
    const downloading = (progress: number) => ({ kind: "downloading", update: artifact, progress });
    const app = await shows(downloading(0.1));
    expect(app.texts()).toContain("10%");
    await app.host.send("test.state", downloading(0.5));
    expect(app.texts()).toContain("50%");
    expect(app.texts()).not.toContain("10%");
    await app.host.send("test.state", { kind: "ready", update: artifact, fileName: APK });
    expect(app.texts()).toContain("Ready to install");
    expect(app.count(ACTION)).toBe(1);
  });

  test("hidden is no row", async () => {
    const app = await shows({ kind: "hidden" });
    expect(app.count("id:app-update")).toBe(0);
  });
});

describe("About update row on iOS", () => {
  const testflight = JSON.stringify({
    schema_version: 1,
    product: "mewla",
    platform: "ios",
    distribution: "testflight",
    bundle_identifier: "com.daoleno.mewla.preview",
    marketing_version: "0.3.0",
    build_number: 141,
    app_id: "6790486999",
  });

  test("a TestFlight build learns of a newer build and Update opens TestFlight", async () => {
    const app = await open(HARNESS, {
      platform: "ios",
      host: {
        handlers: {
          "update.info": { distribution: "testflight", appId: "com.daoleno.mewla.preview", version: "0.3.0", build: "140" },
          "test.fetch": server({ [TESTFLIGHT]: testflight }),
        },
      },
    });
    await app.waitFor("text:Update available");
    expect(app.texts()).toContain("v0.3.0");
    await app.press(ACTION);
    expect(app.openedUrls()).toEqual(["itms-beta://beta.itunes.apple.com/v1/app/6790486999"]);
  });

  test("an App Store build, or one whose host has no update.*, shows nothing", async () => {
    const store = await open(HARNESS, {
      platform: "ios",
      host: { handlers: { "update.info": { distribution: "app-store", appId: "com.daoleno.mewla", version: "0.3.0", build: "140" }, "test.fetch": server({}) } },
    });
    await store.wait(50);
    expect(store.count("id:app-update")).toBe(0);
    // The Play build: no handler answers update.info.
    const play = await open(HARNESS, { platform: "android", host: { handlers: { "test.fetch": server({}) } } });
    await play.wait(50);
    expect(play.count("id:app-update")).toBe(0);
  });
});
