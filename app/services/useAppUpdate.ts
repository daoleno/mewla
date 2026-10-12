import Constants from "expo-constants";
import * as Device from "expo-device";
import { useSyncExternalStore } from "react";
import { AppState, Platform } from "react-native";
import { appVersion } from "../constants/appVersion";
import { getAppUpdateModule } from "../modules/app-update/src";
import {
  ANDROID_UPDATE_ABI,
  type AndroidUpdateArtifact,
  type AppUpdateState,
  appUpdatePending,
  isNewerVersion,
  LATEST_MANIFEST_URL,
  LATEST_SIGNATURE_URL,
  resolveDistributionChannel,
  verifyAndroidUpdateManifest,
} from "./appUpdate";

// One owner for the whole app, so a download keeps going while Settings is closed.
// Checks run on their own (launch, return to foreground) and stay silent; only
// a newer release is ever shown.
const AUTO_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000;
let state: AppUpdateState = initialState();
let lastCheckAt = 0;
const listeners = new Set<() => void>();

function initialState(): AppUpdateState {
  const channel = resolveDistributionChannel(Platform.OS, Constants.expoConfig?.extra);
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

async function check() {
  if (!["idle", "current", "checkFailed"].includes(state.kind)) return;
  // A failed check waits for the next scheduled one, like a successful one.
  if (state.kind !== "idle" && Date.now() - lastCheckAt < AUTO_CHECK_INTERVAL_MS) return;
  lastCheckAt = Date.now();
  setState({ kind: "checking" });
  try {
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
