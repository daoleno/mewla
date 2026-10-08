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
