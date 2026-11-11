import nacl from "../vendor/nacl";
import { compareVersions, parseVersion } from "./version";

/**
 * Build-time distribution channel. GitHub APK builds update themselves from
 * the signed release manifest; TestFlight builds are told when a newer build is
 * installable there and open TestFlight; store builds and "none" (local builds)
 * leave updates to the store and show nothing.
 *
 * The host decides it (`update.info`): Android's GitHub build answers
 * "github-apk" (`-Pmewla.channel=github`); the Play build has no `update.*` at all.
 * iOS answers its `MEWLA_IOS_DISTRIBUTION` build setting.
 */
export type DistributionChannel = "github-apk" | "play-store" | "app-store" | "testflight" | "none";

const CHANNELS: readonly DistributionChannel[] = ["github-apk", "play-store", "app-store", "testflight", "none"];

export const ANDROID_PACKAGE = "com.daoleno.mewla";
export const ANDROID_UPDATE_ABI = "arm64-v8a";
export const RELEASES_URL = "https://github.com/daoleno/mewla/releases";
export const LATEST_MANIFEST_URL = `${RELEASES_URL}/latest/download/release-manifest.json`;
export const LATEST_SIGNATURE_URL = `${LATEST_MANIFEST_URL}.sig`;
// The same Ed25519 key the daemon's selfupdate trusts (PKIX, base64).
export const UPDATE_PUBLIC_KEY_PKIX = "MCowBQYDK2VwAyEAD+GvALNRsWlmuB8FL5nwWchsLuXI7tasTGYSqkdabzw=";
const ED25519_PKIX_PREFIX = [0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00];
const MAX_APK_BYTES = 512 << 20;

// CI publishes this signed file to a rolling release once TestFlight really offers a build
// to testers (not merely once the build uploaded); see .github/workflows/ios-testflight-available.yml.
export const TESTFLIGHT_RELEASE_URL = `${RELEASES_URL}/download/ios-testflight`;
export const TESTFLIGHT_MANIFEST_URL = `${TESTFLIGHT_RELEASE_URL}/testflight-manifest.json`;
export const TESTFLIGHT_SIGNATURE_URL = `${TESTFLIGHT_MANIFEST_URL}.sig`;
const TESTFLIGHT_PUBLIC_LINK = /^https:\/\/testflight\.apple\.com\/join\/[A-Za-z0-9]+$/;

/** What the host says about the running binary (`update.info`). */
export interface UpdateHostInfo {
  /** The channel the build was made for (see DistributionChannel). */
  distribution: string;
  /** The Android package / iOS bundle identifier. */
  appId: string;
  /** versionName / CFBundleShortVersionString. */
  version: string;
  /** versionCode / CFBundleVersion. */
  build: string;
  /** Android: whether the device runs arm64-v8a APKs. */
  arm64?: boolean;
}

/**
 * The channel a build was made for, applied only on the platform that can honour it.
 * No host answer (the Play build, the web, a build without the call) is "none".
 */
export function resolveDistributionChannel(platform: string, configured: unknown): DistributionChannel {
  if (typeof configured !== "string" || !(CHANNELS as readonly string[]).includes(configured)) return "none";
  const channel = configured as DistributionChannel;
  if (platform === "android" && (channel === "github-apk" || channel === "play-store")) return channel;
  if (platform === "ios" && (channel === "app-store" || channel === "testflight")) return channel;
  return "none";
}

export { compareVersions, isNewerVersion, parseVersion } from "./version";

export interface AndroidUpdateArtifact {
  version: string;
  versionCode: number;
  fileName: string;
  url: string;
  sha256: string;
  size: number;
}

export function publicKeyFromPKIX(pkix: string): Uint8Array {
  const der = base64Bytes(pkix);
  if (der.length !== ED25519_PKIX_PREFIX.length + 32 || ED25519_PKIX_PREFIX.some((byte, index) => der[index] !== byte)) {
    throw new Error("Update key is not an Ed25519 public key.");
  }
  return der.slice(ED25519_PKIX_PREFIX.length);
}

function base64Bytes(text: string): Uint8Array {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

/**
 * Verify the release manifest signature over its exact bytes, then read the
 * Android sideload APK it vouches for. Throws on anything not trustworthy;
 * null means the release carries no APK (yet), which is not an update.
 */
export function verifyAndroidUpdateManifest(
  manifestBytes: Uint8Array,
  signature: Uint8Array,
  publicKey: Uint8Array = publicKeyFromPKIX(UPDATE_PUBLIC_KEY_PKIX),
): AndroidUpdateArtifact | null {
  if (signature.length !== nacl.sign.signatureLength || !nacl.sign.detached.verify(manifestBytes, signature, publicKey)) {
    throw new Error("Release signature didn't verify.");
  }
  return parseAndroidUpdateManifest(new TextDecoder("utf-8", { fatal: true }).decode(manifestBytes));
}

export function parseAndroidUpdateManifest(text: string): AndroidUpdateArtifact | null {
  let manifest: unknown;
  try {
    manifest = JSON.parse(text);
  } catch {
    throw new Error("Release manifest isn't valid JSON.");
  }
  if (!isRecord(manifest) || manifest.schema_version !== 2 || manifest.product !== "mewla") {
    throw new Error("Release manifest isn't supported.");
  }
  const version = manifest.version;
  if (typeof version !== "string" || !parseVersion(version) || version.startsWith("v")) {
    throw new Error("Release manifest has no valid version.");
  }
  const android = manifest.android;
  if (!isRecord(android) || android.package !== ANDROID_PACKAGE || android.version_name !== version) {
    throw new Error("Release manifest doesn't describe this app.");
  }
  const versionCode = android.version_code;
  if (typeof versionCode !== "number" || !Number.isSafeInteger(versionCode) || versionCode < 1) {
    throw new Error("Release manifest has no valid version code.");
  }
  const fileName = `mewla-android-arm64-v${version}.apk`;
  const artifacts: unknown[] = Array.isArray(manifest.artifacts) ? manifest.artifacts : [];
  const artifact = artifacts.find(
    (item): item is Record<string, unknown> =>
      isRecord(item) &&
      item.path === fileName &&
      item.role === "android_sideload_apk" &&
      item.abi === ANDROID_UPDATE_ABI,
  );
  if (!artifact) {
    // Android can attach a few minutes after the desktop archives publish.
    return null;
  }
  const sha256 = artifact.sha256;
  const size = artifact.size;
  if (typeof sha256 !== "string" || !/^[0-9a-f]{64}$/i.test(sha256)) {
    throw new Error("Release APK checksum is invalid.");
  }
  if (typeof size !== "number" || !Number.isSafeInteger(size) || size <= 0 || size > MAX_APK_BYTES) {
    throw new Error("Release APK size is invalid.");
  }
  return {
    version,
    versionCode,
    fileName,
    url: `${RELEASES_URL}/download/v${version}/${fileName}`,
    sha256: sha256.toLowerCase(),
    size,
  };
}

/** The newest build TestFlight offers testers, as the signed manifest vouches for it. */
export interface TestFlightBuild {
  /** Numeric marketing version (CFBundleShortVersionString), e.g. 0.2.88. */
  version: string;
  buildNumber: number;
  appId: string;
  publicLink?: string;
}

/** What the running iOS binary is: its numeric marketing version and CFBundleVersion. */
export interface InstalledBuild {
  version: string;
  buildNumber: number;
}

/**
 * The running binary's own identity. A beta release such as 0.2.88-beta.1 ships
 * marketing version 0.2.88, so the semver app version is reduced the same way the
 * iOS build does. Null when the build number isn't a plain integer (dev builds).
 */
export function installedBuild(appVersion: string, buildNumber: unknown): InstalledBuild | null {
  const version = appVersion.trim().replace(/^v/, "").split(/[-+]/, 1)[0];
  const number = typeof buildNumber === "string" && /^[1-9]\d*$/.test(buildNumber) ? Number(buildNumber) : NaN;
  if (!parseVersion(version) || !Number.isSafeInteger(number)) return null;
  return { version, buildNumber: number };
}

/** Whether TestFlight offers something newer than the running build: version first, then build. */
export function isNewerTestFlightBuild(candidate: TestFlightBuild, installed: InstalledBuild): boolean {
  const order = compareVersions(candidate.version, installed.version);
  if (order === null) return false;
  return order > 0 || (order === 0 && candidate.buildNumber > installed.buildNumber);
}

/**
 * Verify the TestFlight availability manifest signature over its exact bytes, then
 * read the build it names. Throws on anything not trustworthy, including a manifest
 * for another app identity.
 */
export function verifyTestFlightManifest(
  manifestBytes: Uint8Array,
  signature: Uint8Array,
  bundleIdentifier: string,
  publicKey: Uint8Array = publicKeyFromPKIX(UPDATE_PUBLIC_KEY_PKIX),
): TestFlightBuild {
  if (signature.length !== nacl.sign.signatureLength || !nacl.sign.detached.verify(manifestBytes, signature, publicKey)) {
    throw new Error("TestFlight signature didn't verify.");
  }
  return parseTestFlightManifest(new TextDecoder("utf-8", { fatal: true }).decode(manifestBytes), bundleIdentifier);
}

export function parseTestFlightManifest(text: string, bundleIdentifier: string): TestFlightBuild {
  let manifest: unknown;
  try {
    manifest = JSON.parse(text);
  } catch {
    throw new Error("TestFlight manifest isn't valid JSON.");
  }
  if (
    !isRecord(manifest) ||
    manifest.schema_version !== 1 ||
    manifest.product !== "mewla" ||
    manifest.platform !== "ios" ||
    manifest.distribution !== "testflight"
  ) {
    throw new Error("TestFlight manifest isn't supported.");
  }
  if (!bundleIdentifier || manifest.bundle_identifier !== bundleIdentifier) {
    throw new Error("TestFlight manifest doesn't describe this app.");
  }
  const version = manifest.marketing_version;
  if (typeof version !== "string" || !/^\d+\.\d+\.\d+$/.test(version) || !parseVersion(version)) {
    throw new Error("TestFlight manifest has no valid version.");
  }
  const buildNumber = manifest.build_number;
  if (typeof buildNumber !== "number" || !Number.isSafeInteger(buildNumber) || buildNumber < 1) {
    throw new Error("TestFlight manifest has no valid build number.");
  }
  const appId = manifest.app_id;
  if (typeof appId !== "string" || !/^[1-9]\d*$/.test(appId)) {
    throw new Error("TestFlight manifest has no valid app id.");
  }
  const publicLink = manifest.public_link;
  if (publicLink !== undefined && (typeof publicLink !== "string" || !TESTFLIGHT_PUBLIC_LINK.test(publicLink))) {
    throw new Error("TestFlight manifest has an invalid link.");
  }
  return { version, buildNumber, appId, ...(publicLink ? { publicLink } : {}) };
}

/** Where the update button goes, best first: the app's own TestFlight page, then the invite link. */
export function testFlightUrls(build: TestFlightBuild): string[] {
  return [
    `itms-beta://beta.itunes.apple.com/v1/app/${build.appId}`,
    build.publicLink ?? "https://testflight.apple.com",
  ];
}

function isRecord(value: unknown): value is Record<string, any> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export type AppUpdateState =
  | { kind: "hidden" }
  | { kind: "idle" }
  | { kind: "checking" }
  | { kind: "current" }
  | { kind: "checkFailed" }
  | { kind: "available"; update: AndroidUpdateArtifact }
  | { kind: "testflightAvailable"; update: TestFlightBuild }
  | { kind: "downloading"; update: AndroidUpdateArtifact; progress: number }
  | { kind: "needsPermission"; update: AndroidUpdateArtifact; fileName: string }
  | { kind: "ready"; update: AndroidUpdateArtifact; fileName: string }
  | { kind: "failed"; update: AndroidUpdateArtifact; message: string };

/**
 * Where a build starts: "idle" (it checks) only where it can take an update.
 * TestFlight builds only learn of a newer build and open TestFlight; the GitHub APK
 * updates itself, but only as the one app the release manifest describes (a preview
 * or debug identity would install the store app beside itself) and only on arm64,
 * the one ABI the release APK carries.
 */
export function initialUpdateState(platform: string, info: UpdateHostInfo | null): AppUpdateState {
  if (!info) return { kind: "hidden" };
  const channel = resolveDistributionChannel(platform, info.distribution);
  if (channel === "testflight") return { kind: "idle" };
  if (channel !== "github-apk") return { kind: "hidden" };
  if (info.appId !== ANDROID_PACKAGE || info.arm64 !== true) return { kind: "hidden" };
  return { kind: "idle" };
}

/** A newer signed release is waiting for the user: Settings carries the dot. */
export function appUpdatePending(state: AppUpdateState): boolean {
  return "update" in state;
}

// Automatic checks (launch, return to foreground) run at most this often.
export const AUTO_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000;
// A tap checks right away, but not again within this window.
export const MANUAL_CHECK_COOLDOWN_MS = 3 * 1000;

/**
 * Whether a check may start now. Only a settled, update-free state checks;
 * automatic checks keep the six-hour rhythm, a tap only waits out the cooldown.
 */
export function shouldCheckForUpdate(
  state: AppUpdateState,
  lastCheckAt: number,
  now: number,
  manual: boolean,
): boolean {
  if (state.kind === "idle") return true;
  if (state.kind !== "current" && state.kind !== "checkFailed") return false;
  return now - lastCheckAt >= (manual ? MANUAL_CHECK_COOLDOWN_MS : AUTO_CHECK_INTERVAL_MS);
}

/** What the About section's update row shows: a short state, and the one action when there is one. */
export interface AppUpdateRowModel {
  title: string;
  /** The version a waiting update carries. */
  subtitle?: string;
  /** A short state beside the title. */
  value?: string;
  /** The compact button, when the state has an action. */
  action?: string;
  /** Whether tapping does something: the action, or checking again. */
  actionable: boolean;
  busy?: boolean;
  /** Download progress, 0 to 1. */
  progress?: number;
}

/**
 * The update row under the app's version. Null where this build cannot update
 * itself; otherwise it always says where it stands in a few words.
 */
export function appUpdateRow(state: AppUpdateState): AppUpdateRowModel | null {
  switch (state.kind) {
    case "hidden":
      return null;
    case "idle":
    case "checking":
      return { title: "Check for updates", busy: true, actionable: false };
    case "current":
      return { title: "Check for updates", value: "Up to date", actionable: true };
    case "checkFailed":
      return { title: "Check for updates", action: "Retry", actionable: true };
    case "available":
      return { title: "Update available", subtitle: `v${state.update.version}`, action: "Update", actionable: true };
    case "testflightAvailable":
      return { title: "Update available", subtitle: `v${state.update.version}`, action: "Update", actionable: true };
    case "downloading":
      return {
        title: "Downloading",
        value: `${Math.floor(state.progress * 100)}%`,
        progress: state.progress,
        actionable: false,
      };
    case "needsPermission":
      return { title: "Allow app installs", action: "Continue", actionable: true };
    case "ready":
      return { title: "Ready to install", subtitle: `v${state.update.version}`, action: "Install", actionable: true };
    case "failed":
      return { title: "Update failed", action: "Retry", actionable: true };
  }
}
