import { requireNativeModule } from "expo-modules-core";

export interface LiveDiagnostics {
  sdk: number;
  notificationsEnabled: boolean;
  /** Android 16 QPR1+: whether the system will promote this app's live notification; null when it cannot say. */
  promotionAllowed: boolean | null;
}

interface MewlaLiveNativeModule {
  apply(rawLiveState: string): boolean;
  setCurrentServer(serverRef: string): void;
  configureWidget(baseUrl: string, token: string): void;
  clearWidget(): void;
  refreshWidgets(): void;
  /** "none" until provisioned, "ok" while the token works, "revoked" after the daemon refused it. */
  widgetState(): "none" | "ok" | "revoked";
  diagnostics(): LiveDiagnostics;
}

let cached: MewlaLiveNativeModule | null | undefined;

/**
 * Android only. iOS has no counterpart yet (Live Activities wait on an APNs key), so it gets
 * null and the app shows nothing new there; callers treat null as "this platform has no live surface".
 */
export function getMewlaLiveModule(): MewlaLiveNativeModule | null {
  if (cached === undefined) {
    try {
      cached = requireNativeModule<MewlaLiveNativeModule>("MewlaLive");
    } catch {
      cached = null;
    }
  }
  return cached;
}
