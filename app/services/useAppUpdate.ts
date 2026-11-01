import Constants from "expo-constants";
import * as Device from "expo-device";
import { useSyncExternalStore } from "react";
import { AppState, Linking, Platform } from "react-native";
import { appVersion } from "../constants/appVersion";
import { getAppUpdateModule } from "../modules/app-update/src";
import {
  ANDROID_UPDATE_ABI,
  type AndroidUpdateArtifact,
  type AppUpdateState,
  appUpdatePending,
  installedBuild,
  isNewerTestFlightBuild,
  isNewerVersion,
  LATEST_MANIFEST_URL,
  LATEST_SIGNATURE_URL,
  resolveDistributionChannel,
  shouldCheckForUpdate,
  type TestFlightBuild,
  TESTFLIGHT_MANIFEST_URL,
  TESTFLIGHT_SIGNATURE_URL,
  testFlightUrls,
  verifyAndroidUpdateManifest,
  verifyTestFlightManifest,
} from "./appUpdate";

// One owner for the whole app, so a download keeps going while Settings is closed.
// Checks run on their own (launch, return to foreground) and on a tap of the
// About row; only a newer release puts the dot on Settings.
const channel = resolveDistributionChannel(Platform.OS, Constants.expoConfig?.extra);
let state: AppUpdateState = initialState();
let lastCheckAt = 0;
const listeners = new Set<() => void>();

function initialState(): AppUpdateState {
  // TestFlight builds only learn about a newer build and open TestFlight; no native module.
  if (channel === "testflight") return { kind: "idle" };
  if (channel !== "github-apk" || !getAppUpdateModule()) return { kind: "hidden" };
  // The GitHub APK is arm64 only; other devices can't install it.
  if (!Device.supportedCpuArchitectures?.includes(ANDROID_UPDATE_ABI)) return { kind: "hidden" };
  return { kind: "idle" };
}

function setState(next: AppUpdateState) {
  state = next;
  for (const listener of listeners) listener();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

async function fetchBytes(url: string): Promise<Uint8Array> {
  const response = await fetch(url, { headers: { Accept: "application/octet-stream" } });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return new Uint8Array(await response.arrayBuffer());
}

async function fetchLatestAndroidUpdate(): Promise<AndroidUpdateArtifact | null> {
  const [manifest, signature] = await Promise.all([
    fetchBytes(LATEST_MANIFEST_URL),
    fetchBytes(LATEST_SIGNATURE_URL),
  ]);
  return verifyAndroidUpdateManifest(manifest, signature);
}

/** The newest TestFlight build testers can install, or null when it isn't newer than this one. */
async function fetchNewerTestFlightBuild(): Promise<TestFlightBuild | null> {
  const bundleIdentifier = Constants.expoConfig?.ios?.bundleIdentifier ?? "";
  const installed = installedBuild(appVersion, Constants.expoConfig?.ios?.buildNumber);
  if (!installed) return null;
  const [manifest, signature] = await Promise.all([
    fetchBytes(TESTFLIGHT_MANIFEST_URL),
    fetchBytes(TESTFLIGHT_SIGNATURE_URL),
  ]);
  const build = verifyTestFlightManifest(manifest, signature, bundleIdentifier);
  return isNewerTestFlightBuild(build, installed) ? build : null;
}

async function openTestFlight(build: TestFlightBuild) {
  for (const url of testFlightUrls(build)) {
    try {
      await Linking.openURL(url);
      return;
    } catch (error) {
      console.warn("[app-update] couldn't open", url, error);
    }
  }
}

async function check(manual = false) {
  // A failed automatic check waits for the next scheduled one, like a successful one.
  if (!shouldCheckForUpdate(state, lastCheckAt, Date.now(), manual)) return;
  lastCheckAt = Date.now();
  setState({ kind: "checking" });
  try {
    if (channel === "testflight") {
      const build = await fetchNewerTestFlightBuild();
      setState(build ? { kind: "testflightAvailable", update: build } : { kind: "current" });
      return;
    }
    const update = await fetchLatestAndroidUpdate();
    setState(update && isNewerVersion(update.version, appVersion) ? { kind: "available", update } : { kind: "current" });
  } catch (error) {
    console.warn("[app-update] check failed", error);
    setState({ kind: "checkFailed" });
  }
}

async function download(update: AndroidUpdateArtifact) {
  const native = getAppUpdateModule();
  if (!native) return;
  setState({ kind: "downloading", update, progress: 0 });
  const subscription = native.addListener("onDownloadProgress", ({ received, total }) => {
    if (state.kind === "downloading" && total > 0) {
      setState({ kind: "downloading", update, progress: Math.min(received / total, 1) });
    }
  });
  try {
    const { fileName } = await native.download({
      url: update.url,
      fileName: update.fileName,
      sha256: update.sha256,
      size: update.size,
    });
    await install(update, fileName);
  } catch (error) {
    console.warn("[app-update] download failed", error);
    setState({ kind: "failed", update, message: error instanceof Error ? error.message : String(error) });
  } finally {
    subscription.remove();
  }
}

async function install(update: AndroidUpdateArtifact, fileName: string) {
  const native = getAppUpdateModule();
  if (!native) return;
  try {
    if (!native.canInstallPackages()) {
      // Android asks once per app; we pick up again when the user comes back.
      setState({ kind: "needsPermission", update, fileName });
      await native.openInstallSettings();
      return;
    }
    // The installer may be dismissed; the row then offers to open it again.
    setState({ kind: "ready", update, fileName });
    await native.install(fileName);
  } catch (error) {
    console.warn("[app-update] install failed", error);
    setState({ kind: "failed", update, message: error instanceof Error ? error.message : String(error) });
  }
}

AppState.addEventListener("change", (next) => {
  if (next !== "active") return;
  if (state.kind === "needsPermission") {
    if (getAppUpdateModule()?.canInstallPackages()) void install(state.update, state.fileName);
    return;
  }
  void check();
});

/** Check once at launch; returning to the foreground checks again when due. */
export function startAppUpdateChecks() {
  void check();
}

function press() {
  switch (state.kind) {
    case "current":
    case "checkFailed":
      void check(true);
      return;
    case "testflightAvailable":
      void openTestFlight(state.update);
      return;
    case "available":
    case "failed":
      void download(state.update);
      return;
    case "needsPermission":
    case "ready":
      void install(state.update, state.fileName);
      return;
  }
}

/** The current app's update state for the About row. */
export function useAppUpdate() {
  const current = useSyncExternalStore(subscribe, () => state);
  return { state: current, press };
}

/** Whether a newer release is waiting: the dot on the way into Settings. */
export function useAppUpdatePending(): boolean {
  return useSyncExternalStore(subscribe, () => appUpdatePending(state));
}
