import { requireOptionalNativeModule } from "expo-modules-core";

interface TouchCancelNativeModule {
  cancelTouchesBelowGestureRoot(): void;
}

const nativeModule =
  requireOptionalNativeModule<TouchCancelNativeModule>("TouchCancel");

/**
 * Android: sends ACTION_CANCEL to the native views under the gesture-handler
 * root after one of its gestures takes over a touch, so a selectable text's
 * pending long press does not select a word. iOS recognizers already cancel
 * the touches they claim, so there is no iOS module and this is a no-op.
 */
export function cancelTouchesBelowGestureRoot() {
  nativeModule?.cancelTouchesBelowGestureRoot();
}
