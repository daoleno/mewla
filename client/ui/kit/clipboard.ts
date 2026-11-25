import { Clipboard } from "nsh";

/**
 * Puts `text` on the clipboard; rejects when the system clipboard refused it. NSH 0.7 resolves `writeText` with
 * `"failed"` instead of rejecting (longbridge/nsh#2753), so a copy button's "Copied" waits on this, not on the
 * bare call. `"requested"` (iOS, Android, Linux: the platform does not say) counts as written.
 */
export async function writeClipboard(text: string): Promise<void> {
  if ((await Clipboard.writeText(text)) === "failed") throw new Error("The clipboard refused the text");
}
