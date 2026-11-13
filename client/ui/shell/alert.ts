import { AlertDialog } from "nsh/base";

/**
 * The app's questions: NSH's AlertDialog. No haptic: the app has two, a request answered and a pairing
 * code found (soft-final SPEC §16.3), never a failure or a warning.
 */
export function showAlert(options: Parameters<typeof AlertDialog.show>[0]): ReturnType<typeof AlertDialog.show> {
  return AlertDialog.show(options);
}
