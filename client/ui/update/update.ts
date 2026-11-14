import { type Accessor, computed, effect, host, openUrl, platform, signal, lifecycle } from "nsh";
import { appUpdatePending, type AppUpdateState, type UpdateHostInfo } from "../../core/update/appUpdate";
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
 */
export interface AppUpdate {
  state: Accessor<AppUpdateState>;
  /** A newer release waits: the dot on the way into Settings. */
  pending: Accessor<boolean>;
  /** Learns what this build is and checks; returning to the foreground checks again when due. Once. */
  start(): void;
  /** The About row's tap or button. */
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

/** An update owner; the app has one (`appUpdate`), tests make their own with a key and fetch of theirs. */
export function createAppUpdate(overrides: Partial<AppUpdateDeps> = {}): AppUpdate {
  const controller = new AppUpdateController({ ...hostDeps(), ...overrides });
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
    },
    press() {
      void controller.press();
    },
  };
}

export const appUpdate = createAppUpdate();
export const updateState = appUpdate.state;
export const updatePending = appUpdate.pending;

/** Once, at start (App.tsx): checks at launch and when the app comes back, at most every six hours. */
export function startAppUpdate(): void {
  appUpdate.start();
}
