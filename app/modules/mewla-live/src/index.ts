import { requireNativeModule } from "expo-modules-core";

export interface LiveDiagnostics {
  sdk: number;
  notificationsEnabled: boolean;
  /** Android 16 QPR1+: whether the system will promote this app's live notification; null when it cannot say. */
  promotionAllowed: boolean | null;
}

interface MewlaLiveNativeModule {
  /** False on iOS in a build without the Live Activity extension: the surface is absent, not broken. */
  isSupported(): boolean;
  apply(rawLiveState: string): boolean;
  setCurrentServer(serverRef: string): void;
  configureWidget(baseUrl: string, token: string): void;
  /** For a server with no address a widget can fetch (Mewla Link): the widgets show the state the app and pushes apply. */
  configureWidgetPushed(): void;
  clearWidget(): void;
  refreshWidgets(): void;
  /** "none" until provisioned, "ok" while the token works, "revoked" after the daemon refused it. */
  widgetState(): "none" | "ok" | "revoked";
  diagnostics(): LiveDiagnostics;
}

let cached: MewlaLiveNativeModule | null | undefined;

/**
 * Android (notification, widgets) and iOS (Live Activity, widgets). Null where the platform or the
 * build has no live surface; callers treat null as "nothing to draw on".
 */
export function getMewlaLiveModule(): MewlaLiveNativeModule | null {
  if (cached === undefined) {
    try {
      const native = requireNativeModule<MewlaLiveNativeModule>("MewlaLive");
      cached = native.isSupported() ? native : null;
    } catch {
      cached = null;
    }
  }
  return cached;
}
