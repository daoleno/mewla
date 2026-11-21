import {
  type AndroidUpdateArtifact,
  type AppUpdateState,
  isNewerVersion,
  shouldCheckForUpdate,
} from "./appUpdate";

/**
 * Mewla for Mac's own update: NSH's updater replaces the whole `.app` (its daemon and CLI inside) from the signed
 * `latest.json` of the GitHub release, and the app starts again as the new one. The states are the app's update
 * states (`AppUpdateState`), so the same row and the same dot serve every platform; a desktop update has only a
 * version to show, so the artifact's other fields are the ones this installer does not use.
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

/**
 * The same owner the app has on Android (`AppUpdateController`): checks at launch and on return to the foreground, at
 * most every six hours, and on a tap of the row; one tap on an update downloads it and restarts into it.
 */
export class DesktopUpdateController {
  private current: AppUpdateState = { kind: "hidden" };
  private installed: string | null = null;
  private lastCheckAt = 0;
  private started: Promise<void> | null = null;
  private found: DesktopUpdateFound | null = null;
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
    if (this.installed) await this.check();
  }

  /** The row's tap or button. */
  async press(): Promise<void> {
    const state = this.current;
    switch (state.kind) {
      case "current":
      case "checkFailed":
        return this.check(true);
      case "available":
      case "failed":
      case "ready":
        return this.install(state.update);
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
      this.set(found && isNewerVersion(found.version, this.installed) ? { kind: "available", update: desktopArtifact(found.version, found.url) } : { kind: "current" });
    } catch (error) {
      // No release carries the manifest yet, or no network: said in the log, and the row offers to try again.
      this.warn("check failed", error);
      this.set({ kind: "checkFailed" });
    }
  }

  private async install(update: AndroidUpdateArtifact): Promise<void> {
    const found = this.found;
    if (!found) return;
    // An install the updater cannot make (the app runs from a disk image): the release page, by hand.
    if (!found.canInstall) {
      await this.deps.openUrl(found.url).catch((error) => this.warn("couldn't open the release", error));
      return;
    }
    this.set({ kind: "downloading", update, progress: 0 });
    try {
      await this.deps.download(({ received, total }) => {
        if (this.current.kind === "downloading" && total && total > 0) this.set({ kind: "downloading", update, progress: Math.min(received / total, 1) });
      });
      this.set({ kind: "ready", update, fileName: "" });
      await this.deps.installAndRestart();
    } catch (error) {
      this.warn("update failed", error);
      this.set({ kind: "failed", update, message: error instanceof Error ? error.message : String(error) });
    }
  }

  private set(next: AppUpdateState): void {
    this.current = next;
    for (const listener of this.listeners) listener(next);
  }

  private warn(message: string, error: unknown): void {
    this.deps.warn?.(`[app-update] ${message}`, error);
  }
}
