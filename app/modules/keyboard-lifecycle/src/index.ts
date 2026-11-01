import { requireOptionalNativeModule } from "expo-modules-core";

export interface KeyboardForegroundSnapshot {
  revision: number;
  imeVisible: boolean;
  imeHeight: number;
  composerFocused: boolean;
  evidence: string;
}

interface KeyboardLifecycleNativeModule {
  getForegroundSnapshot(
    composerNativeId: string,
    revision: number,
  ): Promise<KeyboardForegroundSnapshot>;
  diagEnabled(): boolean;
  diagLog(message: string): void;
}

const nativeModule =
  requireOptionalNativeModule<KeyboardLifecycleNativeModule>(
    "KeyboardLifecycle",
  );

export function getKeyboardForegroundSnapshot(
  composerNativeId: string,
  revision: number,
) {
  if (!nativeModule) {
    return Promise.resolve({
      revision,
      imeVisible: false,
      imeHeight: 0,
      composerFocused: false,
      evidence: "native_module_unavailable",
    });
  }
  return nativeModule.getForegroundSnapshot(composerNativeId, revision);
}

/**
 * Opt-in diagnostics for a build that has no JS console: iOS launch argument
 * `-MewlaKeyboardDiag YES`, Android `setprop debug.mewla.keyboard 1`.
 */
export const keyboardDiagEnabled: boolean = (() => {
  try {
    return nativeModule?.diagEnabled() === true;
  } catch {
    return false;
  }
})();

export function keyboardDiagLog(message: string): void {
  if (!keyboardDiagEnabled) return;
  try {
    nativeModule?.diagLog(message);
  } catch {
    // Diagnostics never get in the way.
  }
}
