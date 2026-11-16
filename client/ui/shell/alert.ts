import { untracked } from "nsh";
import { AlertDialog } from "nsh/base";
import { sheetsOpen } from "./AppSheet";

/**
 * The app's questions: NSH's AlertDialog. No haptic: the app has two, a request answered and a pairing
 * code found (soft-final SPEC §16.3), never a failure or a warning.
 */
export function showAlert(options: Parameters<typeof AlertDialog.show>[0]): ReturnType<typeof AlertDialog.show> {
  // Counted as open while it asks: What's New waits for it.
  sheetsOpen.set(untracked(sheetsOpen) + 1);
  const shown = AlertDialog.show(options);
  void Promise.resolve(shown).finally(() => sheetsOpen.set(untracked(sheetsOpen) - 1)).catch(() => {});
  return shown;
}
