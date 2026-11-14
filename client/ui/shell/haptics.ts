import { Haptics, isAvailable } from "nsh/mobile";
import type { HapticImpact, HapticNotification } from "nsh/mobile";

/**
 * The phone's haptics at the points the Expo app had them (a long press that starts selecting, a
 * destructive question, a pairing's outcome, Git's buttons). Only phones have them: NSH's web and
 * desktop runtimes have no `Haptics`, so nothing plays there, and a haptic never fails a step.
 */
function play(effect: () => void): void {
  if (!isAvailable) return;
  try {
    effect();
  } catch {
    // A haptic is a nicety.
  }
}

export const haptic = {
  /** A selection changed: a segment, a tab, a toggle. */
  selection: () => play(() => Haptics.selection()),
  /** Something started under the finger (a long press). */
  impact: (style: HapticImpact = "medium") => play(() => Haptics.impact(style)),
  /** A task's outcome; `"warning"` before a destructive question. */
  notification: (kind: HapticNotification = "success") => play(() => Haptics.notification(kind)),
};
