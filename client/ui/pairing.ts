import { Clipboard, platform, signal } from "nsh";
import { AlertDialog } from "nsh/base";
import { PairLatch } from "../core/onboarding";
import { NOT_A_CODE, NOTHING_TO_PASTE, alreadyPairedWith, pairWith, pairingProblem, pairingScopeCopy, readPairingLink, type PairingProblem } from "../core/pairFlow";
import { setServerAutoConnect, type StoredServer } from "../core/servers";
import { addPairedServer, current, servers } from "./session";

/**
 * Pairing, one pipeline for every way in: a scanned code, a pasted link, a `mewla://` link the
 * system opened, Settings' "Pair a computer". One at a time; a refused code in view is reported once.
 */
export const pairingBusy = signal(false);
/** Why the last code did not pair (shown under the scanner); cleared by the next attempt. */
export const pairingIssue = signal<PairingProblem | null>(null);
/** The computer just paired: on a phone the connected step shows over everything until the person goes on. */
export const justPaired = signal<StoredServer | null>(null);
/** Counts finished pairings, on every platform: a pairing page closes when it moves. */
export const pairings = signal(0);
const device = () => (platform.os === "web" ? "browser" : "phone");

const latch = new PairLatch();

export type PairSource = "camera" | "paste" | "link";

export async function pairFromText(raw: string, source: PairSource = "link"): Promise<void> {
  const value = raw.trim();
  if (!latch.claim(value, source === "camera")) return;
  pairingIssue.set(null);
  const payload = readPairingLink(value);
  if (!payload) {
    pairingIssue.set(NOT_A_CODE);
    return latch.release(value);
  }
  if (!alreadyPairedWith(servers(), payload)) {
    const copy = pairingScopeCopy(device());
    const { action } = await AlertDialog.show({
      title: copy.title,
      description: copy.message,
      actions: [
        { id: "cancel", label: "Cancel", role: "cancel" },
        { id: "pair", label: copy.confirm },
      ],
    }).catch(() => ({ action: "cancel" }));
    // Cancel changes nothing, and the same code may be scanned again.
    if (action !== "pair") return latch.release();
  }
  pairingBusy.set(true);
  try {
    await addPairedServer(await pairWith(payload));
    const paired = current();
    if (paired) {
      await setServerAutoConnect(paired.id, true).catch(() => {});
      // A browser goes straight to the app; a phone sees Brain come home first.
      if (device() === "phone") justPaired.set(paired);
    }
    pairings.set(pairings() + 1);
    latch.release();
  } catch (error) {
    pairingIssue.set(pairingProblem(error));
    latch.release(value);
  } finally {
    pairingBusy.set(false);
  }
}

/** "Paste a pairing link": the clipboard's text, or a word about what to copy. */
export async function pasteFromClipboard(): Promise<void> {
  const text = (await Clipboard.readText().catch(() => null))?.trim() ?? "";
  if (!text) return pairingIssue.set(NOTHING_TO_PASTE);
  await pairFromText(text, "paste");
}
