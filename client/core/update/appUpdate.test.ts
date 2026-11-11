import { describe, expect, test } from "bun:test";
import nacl from "../vendor/nacl";
import {
  ANDROID_PACKAGE,
  appUpdatePending,
  appUpdateRow,
  AUTO_CHECK_INTERVAL_MS,
  MANUAL_CHECK_COOLDOWN_MS,
  compareVersions,
  initialUpdateState,
  installedBuild,
  isNewerTestFlightBuild,
  isNewerVersion,
  parseTestFlightManifest,
  testFlightUrls,
  verifyTestFlightManifest,
  parseAndroidUpdateManifest,
  publicKeyFromPKIX,
  resolveDistributionChannel,
  shouldCheckForUpdate,
  UPDATE_PUBLIC_KEY_PKIX,
  verifyAndroidUpdateManifest,
} from "./appUpdate";
import { base64ToBytes, RELEASE_V028_MANIFEST, RELEASE_V028_SIGNATURE } from "./testing/releaseV028";

const realManifest = base64ToBytes(RELEASE_V028_MANIFEST);
const realSignature = base64ToBytes(RELEASE_V028_SIGNATURE);

function manifestText(overrides: Record<string, unknown> = {}, artifact: Record<string, unknown> = {}) {
  return JSON.stringify({
    schema_version: 2,
    product: "mewla",
    version: "0.2.9",
    android: { package: "com.daoleno.mewla", version_name: "0.2.9", version_code: 49 },
    artifacts: [
      { path: "mewla-linux-amd64.tar.gz", role: "daemon_archive", sha256: "a".repeat(64), size: 10 },
      {
        path: "mewla-android-arm64-v0.2.9.apk",
        role: "android_sideload_apk",
        sha256: "AB".repeat(32),
        size: 60_000_000,
        abi: "arm64-v8a",
        status: "staged_not_published",
        ...artifact,
      },
    ],
    notes: { release_status: "staged" },
    ...overrides,
  });
}

describe("app version precedence", () => {
  test("matches the daemon's single-stream SemVer order", () => {
    // Same vectors as daemon/selfupdate TestSemVersionPrecedenceSingleStream.
    const ordered = [
      "0.1.0-beta.2",
      "0.1.0-beta.10",
      "0.1.0-rc.1",
      "0.1.0",
      "0.1.1-beta.1",
      "0.1.1",
      "0.2.0",
    ];
    for (let index = 1; index < ordered.length; index += 1) {
      expect(compareVersions(ordered[index - 1], ordered[index])).toBe(-1);
      expect(compareVersions(ordered[index], ordered[index - 1])).toBe(1);
    }
    expect(compareVersions("v0.2.8", "0.2.8")).toBe(0);
    expect(compareVersions("0.2.8+build.4", "0.2.8")).toBe(0);
    expect(compareVersions("1.0.0-alpha", "1.0.0-alpha.1")).toBe(-1);
    expect(compareVersions("1.0.0-alpha.1", "1.0.0-alpha.beta")).toBe(-1);
  });

  test("only a strictly newer, valid release is an update", () => {
    expect(isNewerVersion("0.2.9", "0.2.8")).toBe(true);
    expect(isNewerVersion("0.2.10", "0.2.9")).toBe(true);
    expect(isNewerVersion("0.2.8", "0.2.8")).toBe(false);
    expect(isNewerVersion("0.2.7", "0.2.8")).toBe(false);
    expect(isNewerVersion("0.2.9", "dev")).toBe(false);
    expect(isNewerVersion("0.2", "0.1.0")).toBe(false);
    expect(isNewerVersion("01.2.3", "0.1.0")).toBe(false);
    expect(isNewerVersion("1.0.0-01", "0.1.0")).toBe(false);
  });
});

describe("signed release manifest", () => {
  test("verifies the real v0.2.8 release with the daemon's update key", () => {
    expect(verifyAndroidUpdateManifest(realManifest, realSignature)).toEqual({
      version: "0.2.8",
      versionCode: 48,
      fileName: "mewla-android-arm64-v0.2.8.apk",
      url: "https://github.com/daoleno/mewla/releases/download/v0.2.8/mewla-android-arm64-v0.2.8.apk",
      sha256: "594dc0b61766f5209ccbaadbafaf419c0559e9e64747e5c828ba809223f7b42b",
      size: 60525709,
    });
  });

  test("rejects a manifest whose bytes changed after signing", () => {
    const tampered = new TextEncoder().encode(
      new TextDecoder().decode(realManifest).replace("594dc0b6", "00000000"),
    );
    expect(() => verifyAndroidUpdateManifest(tampered, realSignature)).toThrow("signature");
    expect(() => verifyAndroidUpdateManifest(realManifest, realSignature.slice(1))).toThrow("signature");
  });

  test("rejects a manifest signed by another key", () => {
    const other = nacl.sign.keyPair();
    const signature = nacl.sign.detached(realManifest, other.secretKey);
    expect(() => verifyAndroidUpdateManifest(realManifest, signature)).toThrow("signature");
    expect(verifyAndroidUpdateManifest(realManifest, signature, other.publicKey)?.version).toBe("0.2.8");
  });

  test("reads the Ed25519 key out of its PKIX wrapper only", () => {
    expect(publicKeyFromPKIX(UPDATE_PUBLIC_KEY_PKIX)).toHaveLength(32);
    expect(() => publicKeyFromPKIX("MCowBQYDK2VuAyEAD+GvALNRsWlmuB8FL5nwWchsLuXI7tasTGYSqkdabzw=")).toThrow();
  });

  test("reads the arm64 sideload APK and its checksum", () => {
    expect(parseAndroidUpdateManifest(manifestText())).toEqual({
      version: "0.2.9",
      versionCode: 49,
      fileName: "mewla-android-arm64-v0.2.9.apk",
      url: "https://github.com/daoleno/mewla/releases/download/v0.2.9/mewla-android-arm64-v0.2.9.apk",
      sha256: "ab".repeat(32),
      size: 60_000_000,
    });
  });

  test("rejects manifests that don't vouch for this app's APK", () => {
    const rejects = (text: string, message: string) =>
      expect(() => parseAndroidUpdateManifest(text)).toThrow(message);
    rejects("{", "JSON");
    rejects(manifestText({ schema_version: 1 }), "supported");
    rejects(manifestText({ product: "other" }), "supported");
    rejects(manifestText({ version: "v0.2.9" }), "version");
    rejects(
      manifestText({ android: { package: "com.example", version_name: "0.2.9", version_code: 49 } }),
      "this app",
    );
    rejects(
      manifestText({ android: { package: "com.daoleno.mewla", version_name: "0.2.8", version_code: 49 } }),
      "this app",
    );
    rejects(
      manifestText({ android: { package: "com.daoleno.mewla", version_name: "0.2.9", version_code: 0 } }),
      "version code",
    );
    // No arm64 sideload APK for this exact version: not an update, not an error.
    expect(parseAndroidUpdateManifest(manifestText({}, { abi: "x86_64" }))).toBeNull();
    expect(parseAndroidUpdateManifest(manifestText({}, { path: "mewla-android-arm64-v0.2.8.apk" }))).toBeNull();
    expect(parseAndroidUpdateManifest(manifestText({}, { role: "daemon_archive" }))).toBeNull();
    rejects(manifestText({}, { sha256: "abc" }), "checksum");
    rejects(manifestText({}, { sha256: "z".repeat(64) }), "checksum");
    rejects(manifestText({}, { size: 0 }), "size");
    rejects(manifestText({}, { size: "60" }), "size");
    rejects(manifestText({}, { size: 513 * 1024 * 1024 }), "size");
  });
});

describe("distribution channel", () => {
  test("no channel from the host is nothing actionable", () => {
    // The Expo app defaulted Android to the GitHub APK; here Gradle decides it
    // (-Pmewla.channel), and the Play build answers no update.* call at all.
    expect(resolveDistributionChannel("android", undefined)).toBe("none");
    expect(resolveDistributionChannel("ios", undefined)).toBe("none");
    expect(resolveDistributionChannel("web", "github-apk")).toBe("none");
  });

  test("honours a build-time channel only on the platform it fits", () => {
    expect(resolveDistributionChannel("android", "github-apk")).toBe("github-apk");
    expect(resolveDistributionChannel("android", "play-store")).toBe("play-store");
    expect(resolveDistributionChannel("ios", "app-store")).toBe("app-store");
    expect(resolveDistributionChannel("ios", "testflight")).toBe("testflight");
    expect(resolveDistributionChannel("android", "app-store")).toBe("none");
    expect(resolveDistributionChannel("android", "testflight")).toBe("none");
    expect(resolveDistributionChannel("ios", "github-apk")).toBe("none");
    expect(resolveDistributionChannel("android", "none")).toBe("none");
    expect(resolveDistributionChannel("android", "sideload")).toBe("none");
  });

  test("only the arm64 GitHub APK of this app and TestFlight builds show the row", () => {
    const apk = { distribution: "github-apk", appId: ANDROID_PACKAGE, version: "0.3.0", build: "30000", arm64: true };
    expect(initialUpdateState("android", apk)).toEqual({ kind: "idle" });
    expect(initialUpdateState("android", null)).toEqual({ kind: "hidden" });
    expect(initialUpdateState("android", { ...apk, arm64: false })).toEqual({ kind: "hidden" });
    expect(initialUpdateState("android", { ...apk, arm64: undefined })).toEqual({ kind: "hidden" });
    // The preview and debug identities would install the store app beside themselves.
    expect(initialUpdateState("android", { ...apk, appId: "com.daoleno.mewla.preview" })).toEqual({ kind: "hidden" });
    expect(initialUpdateState("android", { ...apk, distribution: "play-store" })).toEqual({ kind: "hidden" });
    const ios = { distribution: "testflight", appId: "com.daoleno.mewla.preview", version: "0.3.0", build: "141" };
    expect(initialUpdateState("ios", ios)).toEqual({ kind: "idle" });
    expect(initialUpdateState("ios", { ...ios, distribution: "app-store" })).toEqual({ kind: "hidden" });
    expect(initialUpdateState("ios", { ...ios, distribution: "none" })).toEqual({ kind: "hidden" });
    expect(initialUpdateState("web", apk)).toEqual({ kind: "hidden" });
  });
});

describe("About update row", () => {
  const update = parseAndroidUpdateManifest(manifestText())!;

  test("a build that cannot update has no row; otherwise it says where it stands", () => {
    expect(appUpdateRow({ kind: "hidden" })).toBeNull();
    expect(appUpdateRow({ kind: "idle" })).toEqual({ title: "Check for updates", busy: true, actionable: false });
    expect(appUpdateRow({ kind: "checking" })).toEqual({ title: "Check for updates", busy: true, actionable: false });
    expect(appUpdateRow({ kind: "current" })).toEqual({ title: "Check for updates", value: "Up to date", actionable: true });
    expect(appUpdateRow({ kind: "checkFailed" })).toEqual({ title: "Check for updates", action: "Retry", actionable: true });
    // Only a waiting release dots Settings.
    for (const kind of ["hidden", "idle", "checking", "current", "checkFailed"] as const) {
      expect(appUpdatePending({ kind })).toBe(false);
    }
  });

  test("a waiting update is a title, its version and one button, never a sentence", () => {
    expect(appUpdateRow({ kind: "available", update })).toEqual({
      title: "Update available",
      subtitle: "v0.2.9",
      action: "Update",
      actionable: true,
    });
    expect(appUpdateRow({ kind: "downloading", update, progress: 0.426 })).toEqual({
      title: "Downloading",
      value: "42%",
      progress: 0.426,
      actionable: false,
    });
    expect(appUpdateRow({ kind: "ready", update, fileName: update.fileName })).toEqual({
      title: "Ready to install",
      subtitle: "v0.2.9",
      action: "Install",
      actionable: true,
    });
    expect(appUpdateRow({ kind: "failed", update, message: "x" })).toEqual({
      title: "Update failed",
      action: "Retry",
      actionable: true,
    });
    expect(appUpdateRow({ kind: "needsPermission", update, fileName: update.fileName })).toEqual({
      title: "Allow app installs",
      action: "Continue",
      actionable: true,
    });
    expect(appUpdatePending({ kind: "available", update })).toBe(true);
    expect(appUpdatePending({ kind: "downloading", update, progress: 0 })).toBe(true);
  });
});

describe("update check pacing", () => {
  const update = parseAndroidUpdateManifest(manifestText())!;
  const last = 1_000_000;

  test("the first check always runs", () => {
    expect(shouldCheckForUpdate({ kind: "idle" }, 0, last, false)).toBe(true);
  });

  test("automatic checks wait six hours, after success or failure", () => {
    for (const kind of ["current", "checkFailed"] as const) {
      expect(shouldCheckForUpdate({ kind }, last, last + AUTO_CHECK_INTERVAL_MS - 1, false)).toBe(false);
      expect(shouldCheckForUpdate({ kind }, last, last + AUTO_CHECK_INTERVAL_MS, false)).toBe(true);
    }
  });

  test("a tap checks right away but not again within the cooldown", () => {
    for (const kind of ["current", "checkFailed"] as const) {
      expect(shouldCheckForUpdate({ kind }, last, last + MANUAL_CHECK_COOLDOWN_MS - 1, true)).toBe(false);
      expect(shouldCheckForUpdate({ kind }, last, last + MANUAL_CHECK_COOLDOWN_MS, true)).toBe(true);
    }
  });

  test("never checks while busy or once an update is waiting", () => {
    const later = last + AUTO_CHECK_INTERVAL_MS;
    for (const state of [
      { kind: "hidden" },
      { kind: "checking" },
      { kind: "available", update },
      { kind: "downloading", update, progress: 0.5 },
      { kind: "ready", update, fileName: update.fileName },
      { kind: "failed", update, message: "x" },
    ] as const) {
      expect(shouldCheckForUpdate(state, last, later, true)).toBe(false);
      expect(shouldCheckForUpdate(state, last, later, false)).toBe(false);
    }
  });
});

describe("TestFlight availability", () => {
  const bundle = "com.daoleno.mewla.preview";
  const keys = nacl.sign.keyPair();
  function manifest(overrides: Record<string, unknown> = {}) {
    return JSON.stringify({
      schema_version: 1,
      product: "mewla",
      platform: "ios",
      distribution: "testflight",
      bundle_identifier: bundle,
      marketing_version: "0.2.88",
      build_number: 130,
      app_id: "6790486999",
      public_link: "https://testflight.apple.com/join/nMQheDCE",
      ...overrides,
    });
  }
  const sign = (text: string) => nacl.sign.detached(new TextEncoder().encode(text), keys.secretKey);

  test("reads a signed manifest for this app", () => {
    const text = manifest();
    const bytes = new TextEncoder().encode(text);
    expect(verifyTestFlightManifest(bytes, sign(text), bundle, keys.publicKey)).toEqual({
      version: "0.2.88",
      buildNumber: 130,
      appId: "6790486999",
      publicLink: "https://testflight.apple.com/join/nMQheDCE",
    });
  });

  test("rejects a bad signature, another app, and malformed fields", () => {
    const text = manifest();
    const bytes = new TextEncoder().encode(text);
    const tampered = new TextEncoder().encode(manifest({ build_number: 131 }));
    expect(() => verifyTestFlightManifest(tampered, sign(text), bundle, keys.publicKey)).toThrow("signature");
    expect(() => verifyTestFlightManifest(bytes, sign(text), bundle)).toThrow("signature");
    expect(() => parseTestFlightManifest(text, "com.daoleno.mewla")).toThrow("this app");
    expect(() => parseTestFlightManifest(text, "")).toThrow("this app");
    expect(() => parseTestFlightManifest(manifest({ marketing_version: "0.2.88-beta.1" }), bundle)).toThrow("version");
    expect(() => parseTestFlightManifest(manifest({ build_number: "130" }), bundle)).toThrow("build number");
    expect(() => parseTestFlightManifest(manifest({ app_id: "abc" }), bundle)).toThrow("app id");
    expect(() => parseTestFlightManifest(manifest({ public_link: "https://evil.example/join/x" }), bundle)).toThrow("link");
    expect(() => parseTestFlightManifest(manifest({ platform: "android" }), bundle)).toThrow("supported");
    expect(() => parseTestFlightManifest("nope", bundle)).toThrow("JSON");
    expect(parseTestFlightManifest(manifest({ public_link: undefined }), bundle).publicLink).toBeUndefined();
  });

  test("decides availability from the marketing version, then the build number", () => {
    const build = (version: string, buildNumber: number) => ({ version, buildNumber, appId: "1" });
    const installed = installedBuild("0.2.87", "126")!;
    expect(isNewerTestFlightBuild(build("0.2.88", 1), installed)).toBe(true);
    expect(isNewerTestFlightBuild(build("0.2.87", 127), installed)).toBe(true);
    expect(isNewerTestFlightBuild(build("0.2.87", 126), installed)).toBe(false);
    expect(isNewerTestFlightBuild(build("0.2.86", 999), installed)).toBe(false);
  });

  test("a beta binary compares by its marketing version, so it never announces itself", () => {
    const installed = installedBuild("0.2.88-beta.1", "130")!;
    expect(installed).toEqual({ version: "0.2.88", buildNumber: 130 });
    expect(isNewerTestFlightBuild({ version: "0.2.88", buildNumber: 130, appId: "1" }, installed)).toBe(false);
    expect(isNewerTestFlightBuild({ version: "0.2.88", buildNumber: 131, appId: "1" }, installed)).toBe(true);
  });

  test("builds without a real version or build number stay silent", () => {
    expect(installedBuild("dev", "1")).toBeNull();
    expect(installedBuild("0.2.87", undefined)).toBeNull();
    expect(installedBuild("0.2.87", "1.5")).toBeNull();
  });

  test("the row matches Android's, and tapping opens TestFlight's app page before the invite link", () => {
    const update = { version: "0.2.88", buildNumber: 130, appId: "6790486999", publicLink: "https://testflight.apple.com/join/nMQheDCE" };
    const state = { kind: "testflightAvailable", update } as const;
    expect(appUpdateRow(state)).toEqual({ title: "Update available", subtitle: "v0.2.88", action: "Update", actionable: true });
    expect(appUpdatePending(state)).toBe(true);
    expect(testFlightUrls(update)).toEqual([
      "itms-beta://beta.itunes.apple.com/v1/app/6790486999",
      "https://testflight.apple.com/join/nMQheDCE",
    ]);
    expect(testFlightUrls({ ...update, publicLink: undefined })[1]).toBe("https://testflight.apple.com");
  });
});
