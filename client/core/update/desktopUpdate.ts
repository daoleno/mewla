import {
  AUTO_CHECK_INTERVAL_MS,
  type AndroidUpdateArtifact,
  type AppUpdateState,
  isNewerVersion,
  shouldCheckForUpdate,
} from "./appUpdate";

/**
 * Mewla for Mac's own update, the way desktop apps do it: NSH's updater downloads the newer `.app` (its daemon and CLI
 * inside, from the signed `latest.json` of the GitHub release) on its own and stages it to replace the app when it
 * quits (`installOnQuit`), so nobody stays stale; the header's Update button, once it is `ready`, installs it now and
 * starts the new app. The states are the app's update states (`AppUpdateState`), so the same row serves every
 * platform; a desktop update has only a version to show, so the artifact's other fields are the ones this installer
 * does not use.
 */

/** A newer version of the app, as NSH's `checkForUpdate` answers. */
export interface DesktopUpdateFound {
  version: string;
  /** Whether this install can be replaced by the updater (an app in /Applications can; one run from a disk image cannot). */
  canInstall: boolean;
  /** Where the person gets it by hand when `canInstall` is false. */
  url: string;
}

/** What the controller needs of the host: NSH's `nsh/updater` in the app, a fake in tests. */
export interface DesktopUpdater {
  /** The version this app was packaged as; null when it has no update source (Mewla Dev, a development run). */
  installedVersion(): string | null;
  check(): Promise<DesktopUpdateFound | null>;
  download(onProgress: (progress: { received: number; total?: number | null }) => void): Promise<void>;
  /** The downloaded update replaces the app when it quits (the next start opens the new one); settles once it waits. */
  installOnQuit(): Promise<void>;
  /** The app quits and the new one starts, so this settles only on failure. */
  installAndRestart(): Promise<void>;
  openUrl(url: string): Promise<boolean>;
  now(): number;
  warn?(message: string, error?: unknown): void;
}

/** The update as a row shows it: the version, and the fields an installer of another platform needs left empty. */
export function desktopArtifact(version: string, url = ""): AndroidUpdateArtifact {
  return { version, versionCode: 0, fileName: "", url, sha256: "", size: 0 };
}

/** The header's Update button: the update waits downloaded (a click restarts into it), or its install failed (a click tries again). */
export function desktopUpdateButton(state: AppUpdateState): boolean {
  return state.kind === "ready" || state.kind === "failed";
}

/**
 * The updater reads `releases/latest/download/latest.json`; a release published without the Mac files answers that with
 * `HTTP 404`, and NSH rejects as "<manifest url>: HTTP 404". That is "no update here", not a failed check (a manifest
 * without its signature, `latest.json.sig: HTTP 404`, is a broken release and stays one).
 */
export function isNoMacFiles(error: unknown): boolean {
  const message = error instanceof Error ? error.message : String(error);
  return /\/latest\.json: HTTP 404\b/.test(message);
}

/**
 * The same owner the app has on Android (`AppUpdateController`): checks at launch and on return to the foreground, at
 * most every six hours, and on a tap of the row. A newer release downloads at once and waits for the app's quit
 * (`ready`); the Update button restarts into it now. A failed download is tried again at the next check's time.
 */
export class DesktopUpdateController {
  private current: AppUpdateState = { kind: "hidden" };
  private installed: string | null = null;
  private lastCheckAt = 0;
  private started: Promise<void> | null = null;
  private found: DesktopUpdateFound | null = null;
  /** The update is downloaded and waits for the app's quit. */
  private staged = false;
  private restarting = false;
  private failedAt = 0;
  private readonly listeners = new Set<(state: AppUpdateState) => void>();

  constructor(private readonly deps: DesktopUpdater) {}

  get state(): AppUpdateState {
    return this.current;
  }

  subscribe(listener: (state: AppUpdateState) => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  /** Learns whether this build has an update source, then checks once; later calls wait for the same start. */
  start(): Promise<void> {
    this.started ??= (async () => {
      this.installed = this.deps.installedVersion();
      if (!this.installed) return;
      this.set({ kind: "idle" });
      await this.check();
    })();
    return this.started;
  }

  async foreground(): Promise<void> {
    await this.started;
    if (!this.installed) return;
    const state = this.current;
    // A download that failed is tried again when a check would be due, as the next check is.
    if (state.kind === "failed") {
      if (!this.staged && this.deps.now() - this.failedAt >= AUTO_CHECK_INTERVAL_MS) await this.stage(state.update);
      return;
    }
    await this.check();
  }

  /** The row's tap or button: Update (restart into the downloaded app), Retry, or check again. */
  async press(): Promise<void> {
    const state = this.current;
    switch (state.kind) {
      case "current":
      case "checkFailed":
        return this.check(true);
      case "available":
        // An install the updater cannot make (the app runs from a disk image): the release page, by hand.
        return this.openRelease();
      case "failed":
        if (!this.staged) await this.stage(state.update);
        if (this.staged) await this.restart(state.update);
        return;
      case "ready":
        return this.restart(state.update);
    }
  }

  async check(manual = false): Promise<void> {
    if (!this.installed) return;
    if (!shouldCheckForUpdate(this.current, this.lastCheckAt, this.deps.now(), manual)) return;
    this.lastCheckAt = this.deps.now();
    this.set({ kind: "checking" });
    try {
      const found = await this.deps.check();
      this.found = found;
      if (!found || !isNewerVersion(found.version, this.installed)) {
        this.set({ kind: "current" });
        return;
      }
      const update = desktopArtifact(found.version, found.url);
      // An app the updater cannot replace (run from a disk image) only offers the release page.
      if (!found.canInstall) {
        this.set({ kind: "available", update });
        return;
      }
      await this.stage(update);
    } catch (error) {
      // The latest release has no Mac files (no manifest to read): that is no update, not a fault.
      if (isNoMacFiles(error)) {
        this.found = null;
        this.set({ kind: "current" });
        return;
      }
      // No network, or a manifest that is not acceptable: said in the log, and the row offers to try again.
      this.warn("check failed", error);
      this.set({ kind: "checkFailed" });
    }
  }

  /** Downloads the update and stages it for the app's quit; `ready` once it waits. */
  private async stage(update: AndroidUpdateArtifact): Promise<void> {
    this.staged = false;
    this.set({ kind: "downloading", update, progress: 0 });
    try {
      await this.deps.download(({ received, total }) => {
        if (this.current.kind === "downloading" && total && total > 0) this.set({ kind: "downloading", update, progress: Math.min(received / total, 1) });
      });
      await this.deps.installOnQuit();
      this.staged = true;
      this.set({ kind: "ready", update, fileName: "" });
    } catch (error) {
      this.fail(update, "update failed", error);
    }
  }

  /** The app quits and starts again as the new one: this settles only when it could not. */
  private async restart(update: AndroidUpdateArtifact): Promise<void> {
    if (this.restarting) return;
    this.restarting = true;
    try {
      await this.deps.installAndRestart();
    } catch (error) {
      this.fail(update, "restart failed", error);
    } finally {
      this.restarting = false;
    }
  }

  private async openRelease(): Promise<void> {
    const found = this.found;
    if (found) await this.deps.openUrl(found.url).catch((error) => this.warn("couldn't open the release", error));
  }

  private fail(update: AndroidUpdateArtifact, what: string, error: unknown): void {
    this.warn(what, error);
    this.failedAt = this.deps.now();
    this.set({ kind: "failed", update, message: error instanceof Error ? error.message : String(error) });
  }

  private set(next: AppUpdateState): void {
    this.current = next;
    for (const listener of this.listeners) listener(next);
  }

  private warn(message: string, error: unknown): void {
    this.deps.warn?.(`[app-update] ${message}`, error);
  }
}
