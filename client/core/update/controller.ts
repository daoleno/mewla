import {
  type AndroidUpdateArtifact,
  type AppUpdateState,
  initialUpdateState,
  installedBuild,
  isNewerTestFlightBuild,
  isNewerVersion,
  LATEST_MANIFEST_URL,
  LATEST_SIGNATURE_URL,
  publicKeyFromPKIX,
  resolveDistributionChannel,
  shouldCheckForUpdate,
  type TestFlightBuild,
  TESTFLIGHT_MANIFEST_URL,
  TESTFLIGHT_SIGNATURE_URL,
  testFlightUrls,
  UPDATE_PUBLIC_KEY_PKIX,
  type UpdateHostInfo,
  verifyAndroidUpdateManifest,
  verifyTestFlightManifest,
} from "./appUpdate";

export interface DownloadRequest {
  url: string;
  fileName: string;
  sha256: string;
  size: number;
}

export interface DownloadProgress {
  received: number;
  total: number;
}

/** The Android host's installer (`update.*` of the GitHub APK). */
export interface AppInstaller {
  /** Downloads into the app's cache and keeps the file only when its size and SHA-256 match. */
  download(request: DownloadRequest): Promise<{ fileName: string }>;
  /** Listens to the download's progress until the returned function is called. */
  onProgress(listener: (progress: DownloadProgress) => void): () => void;
  canInstall(): Promise<boolean>;
  openInstallSettings(): Promise<void>;
  install(fileName: string): Promise<void>;
}

export interface AppUpdateDeps {
  platform: string;
  /** What the host says about this binary; null (or a rejection) where it has no `update.*`. */
  info(): Promise<UpdateHostInfo | null>;
  /** Android only; null elsewhere. */
  installer: AppInstaller | null;
  fetchBytes(url: string): Promise<Uint8Array>;
  /** Opens a URL outside the app: whether something opened it. */
  openUrl(url: string): Promise<boolean>;
  now(): number;
  /** The release key; the real one unless a test signs its own manifests. */
  publicKey?: Uint8Array;
  warn?(message: string, error?: unknown): void;
}

/**
 * The app's one update owner, so a download keeps going while Settings is closed.
 * Checks run on their own (launch, return to the foreground, at most every six
 * hours) and on a tap of the About row; only a newer release makes it pending.
 */
export class AppUpdateController {
  private current: AppUpdateState = { kind: "hidden" };
  private info: UpdateHostInfo | null = null;
  private lastCheckAt = 0;
  private started: Promise<void> | null = null;
  private readonly listeners = new Set<(state: AppUpdateState) => void>();
  private readonly publicKey: Uint8Array;

  constructor(private readonly deps: AppUpdateDeps) {
    this.publicKey = deps.publicKey ?? publicKeyFromPKIX(UPDATE_PUBLIC_KEY_PKIX);
  }

  get state(): AppUpdateState {
    return this.current;
  }

  subscribe(listener: (state: AppUpdateState) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  /** Learns what this build is, then checks once; later calls wait for the same start. */
  start(): Promise<void> {
    this.started ??= (async () => {
      this.info = await this.deps.info().catch(() => null);
      this.set(initialUpdateState(this.deps.platform, this.info));
      await this.check();
    })();
    return this.started;
  }

  /** The app came back to the foreground: finish an install the person just allowed, else check when due. */
  async foreground(): Promise<void> {
    await this.started;
    const state = this.current;
    if (state.kind === "needsPermission") {
      if (await this.canInstall()) await this.install(state.update, state.fileName);
      return;
    }
    await this.check();
  }

  /** The About row's tap or button. */
  async press(): Promise<void> {
    const state = this.current;
    switch (state.kind) {
      case "current":
      case "checkFailed":
        return this.check(true);
      case "testflightAvailable":
        return this.openTestFlight(state.update);
      case "available":
      case "failed":
        return this.download(state.update);
      case "needsPermission":
      case "ready":
        return this.install(state.update, state.fileName);
    }
  }

  async check(manual = false): Promise<void> {
    // A failed automatic check waits for the next scheduled one, like a successful one.
    if (!shouldCheckForUpdate(this.current, this.lastCheckAt, this.deps.now(), manual)) return;
    this.lastCheckAt = this.deps.now();
    this.set({ kind: "checking" });
    try {
      if (this.channel() === "testflight") {
        const build = await this.newerTestFlightBuild();
        this.set(build ? { kind: "testflightAvailable", update: build } : { kind: "current" });
        return;
      }
      const update = await this.latestAndroidUpdate();
      const installed = this.info?.version ?? "";
      this.set(update && isNewerVersion(update.version, installed) ? { kind: "available", update } : { kind: "current" });
    } catch (error) {
      this.warn("check failed", error);
      this.set({ kind: "checkFailed" });
    }
  }

  private channel() {
    return resolveDistributionChannel(this.deps.platform, this.info?.distribution);
  }

  private async latestAndroidUpdate(): Promise<AndroidUpdateArtifact | null> {
    const [manifest, signature] = await Promise.all([
      this.deps.fetchBytes(LATEST_MANIFEST_URL),
      this.deps.fetchBytes(LATEST_SIGNATURE_URL),
    ]);
    return verifyAndroidUpdateManifest(manifest, signature, this.publicKey);
  }

  /** The newest TestFlight build testers can install, or null when it isn't newer than this one. */
  private async newerTestFlightBuild(): Promise<TestFlightBuild | null> {
    const installed = installedBuild(this.info?.version ?? "", this.info?.build);
    if (!installed) return null;
    const [manifest, signature] = await Promise.all([
      this.deps.fetchBytes(TESTFLIGHT_MANIFEST_URL),
      this.deps.fetchBytes(TESTFLIGHT_SIGNATURE_URL),
    ]);
    const build = verifyTestFlightManifest(manifest, signature, this.info?.appId ?? "", this.publicKey);
    return isNewerTestFlightBuild(build, installed) ? build : null;
  }

  private async openTestFlight(build: TestFlightBuild): Promise<void> {
    for (const url of testFlightUrls(build)) {
      try {
        if (await this.deps.openUrl(url)) return;
      } catch (error) {
        this.warn(`couldn't open ${url}`, error);
      }
    }
  }

  private async download(update: AndroidUpdateArtifact): Promise<void> {
    const installer = this.deps.installer;
    if (!installer) return;
    this.set({ kind: "downloading", update, progress: 0 });
    const stop = installer.onProgress(({ received, total }) => {
      if (this.current.kind === "downloading" && total > 0) {
        this.set({ kind: "downloading", update, progress: Math.min(received / total, 1) });
      }
    });
    try {
      const { fileName } = await installer.download({
        url: update.url,
        fileName: update.fileName,
        sha256: update.sha256,
        size: update.size,
      });
      stop();
      await this.install(update, fileName);
    } catch (error) {
      this.warn("download failed", error);
      this.set({ kind: "failed", update, message: messageOf(error) });
    } finally {
      stop();
    }
  }

  private async install(update: AndroidUpdateArtifact, fileName: string): Promise<void> {
    const installer = this.deps.installer;
    if (!installer) return;
    try {
      if (!(await installer.canInstall())) {
        // Android asks once per app; we pick up again when the person comes back.
        this.set({ kind: "needsPermission", update, fileName });
        await installer.openInstallSettings();
        return;
      }
      // The installer may be dismissed; the row then offers to open it again.
      this.set({ kind: "ready", update, fileName });
      await installer.install(fileName);
    } catch (error) {
      this.warn("install failed", error);
      this.set({ kind: "failed", update, message: messageOf(error) });
    }
  }

  private canInstall(): Promise<boolean> {
    return this.deps.installer?.canInstall().catch(() => false) ?? Promise.resolve(false);
  }

  private set(next: AppUpdateState): void {
    this.current = next;
    for (const listener of this.listeners) listener(next);
  }

  private warn(message: string, error: unknown): void {
    this.deps.warn?.(`[app-update] ${message}`, error);
  }
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}
