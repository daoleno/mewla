import { toByteArray } from "base64-js";
import nacl from "tweetnacl";

/**
 * Build-time distribution channel. GitHub APK builds update themselves from
 * the signed release manifest; store builds and "none" (iOS today, local
 * builds) leave updates to the store and show nothing.
 */
export type DistributionChannel = "github-apk" | "play-store" | "app-store" | "none";

const CHANNELS: readonly DistributionChannel[] = ["github-apk", "play-store", "app-store", "none"];

export const ANDROID_PACKAGE = "com.daoleno.mewla";
export const ANDROID_UPDATE_ABI = "arm64-v8a";
export const RELEASES_URL = "https://github.com/daoleno/mewla/releases";
export const LATEST_MANIFEST_URL = `${RELEASES_URL}/latest/download/release-manifest.json`;
export const LATEST_SIGNATURE_URL = `${LATEST_MANIFEST_URL}.sig`;
// The same Ed25519 key the daemon's selfupdate trusts (PKIX, base64).
export const UPDATE_PUBLIC_KEY_PKIX = "MCowBQYDK2VwAyEAD+GvALNRsWlmuB8FL5nwWchsLuXI7tasTGYSqkdabzw=";
const ED25519_PKIX_PREFIX = [0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00];
const MAX_APK_BYTES = 512 << 20;

export function resolveDistributionChannel(platform: string, extra: unknown): DistributionChannel {
  const distribution = isRecord(extra) && isRecord(extra.distribution) ? extra.distribution : {};
  const configured = platform === "android" || platform === "ios" ? distribution[platform] : undefined;
  if (typeof configured === "string" && (CHANNELS as readonly string[]).includes(configured)) {
    const channel = configured as DistributionChannel;
    // A channel only applies to the platform that can honour it.
    if (platform === "android" && (channel === "github-apk" || channel === "play-store")) return channel;
    if (platform === "ios" && channel === "app-store") return channel;
    return "none";
  }
  return platform === "android" ? "github-apk" : "none";
}

interface SemVersion {
  core: [number, number, number];
  prerelease: string[];
}

export function parseVersion(raw: string): SemVersion | null {
  const value = raw.trim().replace(/^v/, "").split("+", 1)[0];
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(.+))?$/.exec(value);
  if (!match) return null;
  const prerelease = match[4] === undefined ? [] : match[4].split(".");
  for (const part of prerelease) {
    if (!/^[0-9A-Za-z-]+$/.test(part) || (/^\d+$/.test(part) && part.length > 1 && part[0] === "0")) {
      return null;
    }
  }
  const core = [Number(match[1]), Number(match[2]), Number(match[3])] as [number, number, number];
  if (!core.every(Number.isSafeInteger)) return null;
  return { core, prerelease };
}

/** SemVer precedence, matching daemon/selfupdate.compareSemVersion. */
export function compareVersions(left: string, right: string): number | null {
  const a = parseVersion(left);
  const b = parseVersion(right);
  if (!a || !b) return null;
  for (let index = 0; index < 3; index += 1) {
    if (a.core[index] !== b.core[index]) return a.core[index] < b.core[index] ? -1 : 1;
  }
  if (!a.prerelease.length || !b.prerelease.length) {
    return a.prerelease.length === b.prerelease.length ? 0 : a.prerelease.length ? -1 : 1;
  }
  for (let index = 0; index < Math.min(a.prerelease.length, b.prerelease.length); index += 1) {
    const x = a.prerelease[index];
    const y = b.prerelease[index];
    if (x === y) continue;
    const xNumeric = /^\d+$/.test(x);
    const yNumeric = /^\d+$/.test(y);
    if (xNumeric && yNumeric) {
      return x.length < y.length || (x.length === y.length && x < y) ? -1 : 1;
    }
    if (xNumeric) return -1;
    if (yNumeric) return 1;
    return x < y ? -1 : 1;
  }
  return Math.sign(a.prerelease.length - b.prerelease.length);
}

export function isNewerVersion(candidate: string, current: string): boolean {
  return compareVersions(candidate, current) === 1;
}

export interface AndroidUpdateArtifact {
  version: string;
  versionCode: number;
  fileName: string;
  url: string;
  sha256: string;
  size: number;
}

export function publicKeyFromPKIX(pkix: string): Uint8Array {
  const der = toByteArray(pkix);
  if (der.length !== ED25519_PKIX_PREFIX.length + 32 || ED25519_PKIX_PREFIX.some((byte, index) => der[index] !== byte)) {
    throw new Error("Update key is not an Ed25519 public key.");
  }
  return der.slice(ED25519_PKIX_PREFIX.length);
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
  const artifacts = Array.isArray(manifest.artifacts) ? manifest.artifacts : [];
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
  | { kind: "downloading"; update: AndroidUpdateArtifact; progress: number }
  | { kind: "needsPermission"; update: AndroidUpdateArtifact; fileName: string }
  | { kind: "ready"; update: AndroidUpdateArtifact; fileName: string }
  | { kind: "failed"; update: AndroidUpdateArtifact; message: string };

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

/**
 * The About row's status line, and whether tapping the row does something.
 * Builds that can update themselves always say where they stand, and a tap
 * checks again; builds that can't show the version only.
 */
export function appUpdateStatus(state: AppUpdateState): { text: string | null; actionable: boolean } {
  switch (state.kind) {
    case "hidden":
      return { text: null, actionable: false };
    case "idle":
    case "checking":
      return { text: "Checking for updates…", actionable: false };
    case "current":
      return { text: "Up to date · Tap to check", actionable: true };
    case "checkFailed":
      return { text: "Couldn't check for updates · Tap to retry", actionable: true };
    case "available":
      return { text: `Update to v${state.update.version}`, actionable: true };
    case "downloading":
      return { text: `Downloading ${Math.floor(state.progress * 100)}%`, actionable: false };
    case "needsPermission":
      return { text: "Allow Mewla to install apps · Tap to continue", actionable: true };
    case "ready":
      return { text: `Install v${state.update.version}`, actionable: true };
    case "failed":
      return { text: "Couldn't update · Tap to retry", actionable: true };
  }
}
