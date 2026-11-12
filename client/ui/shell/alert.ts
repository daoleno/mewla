import { AlertDialog } from "nsh/base";
import { haptic } from "./haptics";

/**
 * The app's questions: NSH's AlertDialog, with the warning haptic first when an answer is
 * destructive (delete, block, discard, terminate), as the Expo app had it. An action sheet (a
 * menu) only lists choices, so it plays nothing; the question its destructive pick leads to does.
 */
export function showAlert(options: Parameters<typeof AlertDialog.show>[0]): ReturnType<typeof AlertDialog.show> {
  if (options.presentation !== "actionSheet" && options.actions?.some((action) => action.role === "destructive")) haptic.notification("warning");
  return AlertDialog.show(options);
}
