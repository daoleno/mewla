import { type Accessor, computed, effect, host, openUrl, platform, signal, lifecycle } from "nsh";
import { checkForUpdate, downloadUpdate, installAndRestart, installOnQuit, shellInfo } from "nsh/updater";
import { AUTO_CHECK_INTERVAL_MS, appUpdatePending, type AppUpdateState, type UpdateHostInfo } from "../../core/update/appUpdate";
import { DesktopUpdateController, desktopUpdateButton, type DesktopUpdater } from "../../core/update/desktopUpdate";
import { isMacApp } from "../mac/app";
import { AppUpdateController, type AppInstaller, type AppUpdateDeps, type DownloadProgress, type DownloadRequest } from "../../core/update/controller";

/**
 * The app's own update, one owner for the whole app (a download keeps going while
 * Settings is closed): the GitHub APK downloads and installs the signed release,
 * a TestFlight build opens TestFlight, every other build shows nothing.
 *
 * Host calls (`update.*`, only where the build can update): `update.info` →
 * {distribution, appId, version, build, arm64?}; Android's GitHub APK also has
 * `update.download` {url, fileName, sha256, size} → {fileName} with the event
 * `update.progress` {received, total}, `update.canInstall`, `update.openInstallSettings`,
 * `update.install` {fileName}. A failing `update.info` (the Play build, the web) hides it.
 *
 * Mewla for Mac updates by NSH's own updater (`nsh/updater`), the way desktop apps do: a newer release downloads on
 * its own and is staged to replace the whole app (its daemon and CLI inside) when the app quits; the header's Update
 * button (`UpdateButton`) installs it now and starts the new app. The release names that source in its
 * nsh-updater.json (client/macos/package.sh), Mewla Dev has none and shows nothing.
 */
export interface AppUpdate {
  state: Accessor<AppUpdateState>;
  /** A newer release waits: the dot on the way into Settings. */
  pending: Accessor<boolean>;
  /** Learns what this build is and checks; returning to the foreground checks again when due. Once. */
  start(): void;
  /** The About row's tap or button, and the header's Update button. */
  press(): void;
}

// Progress comes as one host event; it goes to the download that listens now.
let progressListener: ((progress: DownloadProgress) => void) | null = null;
let progressBound = false;
function onProgress(listener: (progress: DownloadProgress) => void): () => void {
  if (!progressBound) {
    progressBound = true;
    host.on<DownloadProgress | null>("update.progress", (data) => {
      if (data && typeof data.received === "number" && typeof data.total === "number") progressListener?.(data);
    });
  }
  progressListener = listener;
  return () => {
    if (progressListener === listener) progressListener = null;
  };
}

const androidInstaller: AppInstaller = {
  // The person waits for it as long as it takes; the host checks size and SHA-256.
  download: (request: DownloadRequest) => host.call<{ fileName: string }>("update.download", request, { timeout: 0 }),
  onProgress,
  canInstall: async () => (await host.call<boolean>("update.canInstall")) === true,
  openInstallSettings: async () => void (await host.call("update.openInstallSettings")),
  install: async (fileName: string) => void (await host.call("update.install", { fileName })),
};

async function fetchBytes(url: string): Promise<Uint8Array> {
  const response = await fetch(url, { headers: { Accept: "application/octet-stream" } });
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  return new Uint8Array(await response.arrayBuffer());
}

function hostDeps(): AppUpdateDeps {
  const hosted = host.available() && (platform.os === "android" || platform.os === "ios");
  return {
    platform: platform.os,
    info: async () => (hosted ? await host.call<UpdateHostInfo>("update.info") : null),
    installer: hosted && platform.os === "android" ? androidInstaller : null,
    fetchBytes,
    openUrl: async (url) => (await openUrl(url)).opened,
    now: () => Date.now(),
    warn: (message, error) => console.warn(message, error instanceof Error ? error.message : error),
  };
}

/** NSH's updater, as Mewla for Mac's update controller uses it. */
const nshUpdater: DesktopUpdater = {
  installedVersion: () => shellInfo().version ?? null,
  check: async () => {
    const found = await checkForUpdate();
    return found ? { version: found.version, canInstall: found.canInstall, url: found.url } : null;
  },
  download: (onProgress) => downloadUpdate(onProgress),
  installOnQuit: () => installOnQuit(),
  installAndRestart: () => installAndRestart(),
  openUrl: async (url) => (await openUrl(url)).opened,
  now: () => Date.now(),
  warn: (message, error) => console.warn(message, error instanceof Error ? error.message : error),
};

/** What the owner needs of either controller. */
interface AppUpdateOwner {
  readonly state: AppUpdateState;
  subscribe(listener: (state: AppUpdateState) => void): () => void;
  start(): Promise<void>;
  foreground(): Promise<void>;
  press(): Promise<void>;
}

/** An update owner; the app has one (`appUpdate`), tests make their own with a key and fetch of theirs. */
export function createAppUpdate(overrides: Partial<AppUpdateDeps> = {}, desktop: DesktopUpdater | null = platform.os === "macos" ? nshUpdater : null): AppUpdate {
  const controller: AppUpdateOwner = desktop ? new DesktopUpdateController(desktop) : new AppUpdateController({ ...hostDeps(), ...overrides });
  const state = signal<AppUpdateState>(controller.state);
  controller.subscribe((next) => state.set(next));
  const pending = computed(() => appUpdatePending(state()));
  let started = false;
  return {
    state,
    pending,
    start() {
      if (started) return;
      started = true;
      void controller.start();
      // Reads only the lifecycle: a state change never starts a check.
      effect(() => {
        if (lifecycle() === "active") void controller.foreground();
      });
      // The Mac's app runs for weeks with its window closed: it looks again by itself (the controller keeps the
      // six hours between checks), so the update is downloaded before anyone opens the window.
      if (desktop) setInterval(() => void controller.foreground(), AUTO_CHECK_INTERVAL_MS / 6);
    },
    press() {
      void controller.press();
    },
  };
}

export const appUpdate = createAppUpdate();

/** This app's update owner for what shows it (Settings, the Update button): the app's own; a test binds one of its own. */
export const appUpdateOwner = signal<AppUpdate>(appUpdate);

export function bindAppUpdate(update: AppUpdate): void {
  appUpdateOwner.set(update);
}

/** A newer release of this app waits, until it is installed: Settings' row carries the dot and the update row sits first. */
export const appUpdateWaiting = computed(() => appUpdateOwner().pending());

/** The header's Update button shows (Mewla for Mac): the update waits downloaded, and a press restarts into it. */
export const appUpdateButton = computed(() => isMacApp() && desktopUpdateButton(appUpdateOwner().state()));

/** Once, at start (App.tsx): checks at launch and when the app comes back, at most every six hours. */
export function startAppUpdate(): void {
  appUpdate.start();
}
