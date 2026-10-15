import { describe, expect, test } from "bun:test";
import { readFileSync } from "fs";
import path from "path";
import nacl from "tweetnacl";
import {
  appUpdatePending,
  appUpdateStatus,
  AUTO_CHECK_INTERVAL_MS,
  MANUAL_CHECK_COOLDOWN_MS,
  compareVersions,
  isNewerVersion,
  parseAndroidUpdateManifest,
  publicKeyFromPKIX,
  resolveDistributionChannel,
  shouldCheckForUpdate,
  UPDATE_PUBLIC_KEY_PKIX,
  verifyAndroidUpdateManifest,
} from "./appUpdate";

const fixture = path.join(__dirname, "../__fixtures__/release-v0.2.8");
const realManifest = new Uint8Array(readFileSync(path.join(fixture, "release-manifest.json")));
const realSignature = new Uint8Array(readFileSync(path.join(fixture, "release-manifest.json.sig")));

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
  });
});

describe("distribution channel", () => {
  test("Android defaults to the GitHub APK channel and iOS to nothing actionable", () => {
    expect(resolveDistributionChannel("android", {})).toBe("github-apk");
    expect(resolveDistributionChannel("android", undefined)).toBe("github-apk");
    expect(resolveDistributionChannel("ios", {})).toBe("none");
    expect(resolveDistributionChannel("web", { distribution: { android: "github-apk" } })).toBe("none");
  });

  test("honours a build-time channel only on the platform it fits", () => {
    const extra = { distribution: { android: "play-store", ios: "app-store" } };
    expect(resolveDistributionChannel("android", extra)).toBe("play-store");
    expect(resolveDistributionChannel("ios", extra)).toBe("app-store");
    expect(resolveDistributionChannel("android", { distribution: { android: "app-store" } })).toBe("none");
    expect(resolveDistributionChannel("ios", { distribution: { ios: "github-apk" } })).toBe("none");
    expect(resolveDistributionChannel("android", { distribution: { android: "none" } })).toBe("none");
    expect(resolveDistributionChannel("android", { distribution: { android: "sideload" } })).toBe("github-apk");
  });
});

describe("About row status", () => {
  const update = parseAndroidUpdateManifest(manifestText())!;

  test("always says where an updatable build stands, and a settled check can be tapped", () => {
    expect(appUpdateStatus({ kind: "hidden" })).toEqual({ text: null, actionable: false });
    expect(appUpdateStatus({ kind: "idle" })).toEqual({ text: "Checking for updates…", actionable: false });
    expect(appUpdateStatus({ kind: "checking" })).toEqual({ text: "Checking for updates…", actionable: false });
    expect(appUpdateStatus({ kind: "current" })).toEqual({ text: "Up to date · Tap to check", actionable: true });
    expect(appUpdateStatus({ kind: "checkFailed" })).toEqual({
      text: "Couldn't check for updates · Tap to retry",
      actionable: true,
    });
    // Only a waiting release dots Settings.
    for (const kind of ["hidden", "idle", "checking", "current", "checkFailed"] as const) {
      expect(appUpdatePending({ kind })).toBe(false);
    }
  });

  test("reads short and plain once an update is waiting", () => {
    expect(appUpdateStatus({ kind: "available", update })).toEqual({
      text: "Update to v0.2.9",
      actionable: true,
    });
    expect(appUpdateStatus({ kind: "downloading", update, progress: 0.426 })).toEqual({
      text: "Downloading 42%",
      actionable: false,
    });
    expect(appUpdateStatus({ kind: "ready", update, fileName: update.fileName }).text).toBe("Install v0.2.9");
    expect(appUpdateStatus({ kind: "failed", update, message: "x" })).toEqual({
      text: "Couldn't update · Tap to retry",
      actionable: true,
    });
    expect(appUpdateStatus({ kind: "needsPermission", update, fileName: update.fileName })).toEqual({
      text: "Allow Mewla to install apps · Tap to continue",
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
