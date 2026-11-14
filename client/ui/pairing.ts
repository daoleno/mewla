import { Clipboard, host, platform, signal } from "nsh";
import { PairLatch } from "../core/onboarding";
import { LINK_REQUIRED, NO_CODE_IN_PICTURE, NOT_A_CODE, NOTHING_TO_PASTE, alreadyPairedWith, pairWith, pairingProblem, pairingScopeCopy, pasteRefused, clipboardRead, readPairingLink, supersededAtAddress, type PairingProblem } from "../core/pairFlow";
import { getServers, removeServer, setServerAutoConnect, type StoredServer } from "../core/servers";
import { addPairedServer, current, servers } from "./session";
import { haptic } from "./shell/haptics";
import { showAlert } from "./shell/alert";

/**
 * Pairing, one pipeline for every way in: a scanned code, a pasted link, a `mewla://` link the
 * system opened, Settings' "Pair a computer". One at a time; a refused code in view is reported once.
 */
export const pairingBusy = signal(false);
/** What a slow pairing is doing (Mewla Relay: "Connecting to … through Mewla…"); empty means "Pairing…". */
export const pairingStatus = signal("");
/** Why the last code did not pair (shown under the scanner); cleared by the next attempt. */
export const pairingIssue = signal<PairingProblem | null>(null);
/** The computer just paired: on a phone the connected step shows over everything until the person goes on. */
export const justPaired = signal<StoredServer | null>(null);
/**
 * A pairing link the system opened while the app runs: "Pairing…" (or what a slow pairing is doing)
 * while it pairs, then why it did not. The pairing pages show their own; the app shows this one over
 * the rest, as the Expo app's toasts did (parity #36).
 */
export const linkPairing = signal<{ problem: PairingProblem | null } | null>(null);
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
  // A pairing code found in the finder: one of the app's two light ticks (SPEC §16.3 Haptics).
  if (source === "camera") haptic.impact("light");
  if (!alreadyPairedWith(servers(), payload)) {
    const copy = pairingScopeCopy(device());
    const { action } = await showAlert({
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
  pairingStatus.set("");
  if (source === "link") linkPairing.set({ problem: null });
  try {
    await addPairedServer(await pairWith(payload, (message) => pairingStatus.set(message)));
    const paired = current();
    if (paired) await setServerAutoConnect(paired.id, true).catch(() => {});
    // A browser's page comes from the computer it just paired: an older pairing at that address is gone.
    if (paired && device() === "browser") {
      const stale = supersededAtAddress(servers(), paired);
      for (const id of stale) await removeServer(id).catch(() => {});
      if (stale.length) servers.set(await getServers());
    }
    // Settings' Pair page closes on this, while the app is still there under it.
    pairings.set(pairings() + 1);
    // A browser goes straight to the app; a phone sees Brain come home first (the app goes until then).
    if (paired && device() === "phone") justPaired.set(paired);
    if (source === "link") linkPairing.set(null);
    latch.release();
  } catch (error) {
    pairingIssue.set(pairingProblem(error));
    if (source === "link") linkPairing.set({ problem: pairingProblem(error) });
    latch.release(value);
  } finally {
    pairingBusy.set(false);
    pairingStatus.set("");
  }
}

/** "Paste a pairing link": the clipboard's text, or a word about what to copy. */
export async function pasteFromClipboard(): Promise<void> {
  const read = await readClipboard();
  if (read.kind === "refused") return pairingIssue.set(pasteRefused(read.error));
  if (read.kind === "empty") return pairingIssue.set(NOTHING_TO_PASTE);
  await pairFromText(read.text, "paste");
}

/** The clipboard's text, or that the system refused it, or that it is empty (core/pairFlow.ts `clipboardRead`). */
async function readClipboard(): Promise<ReturnType<typeof clipboardRead>> {
  try {
    const text = await Clipboard.readText();
    // Asked only when no text came: `hasText` shows no paste prompt.
    const has = text === "" ? await Clipboard.hasText().catch(() => false) : true;
    return clipboardRead({ text, has });
  } catch (error) {
    return clipboardRead({ error });
  }
}

/** Settings' "Pair": the link typed or pasted into the field, else the clipboard's. */
export async function pairFromField(text: string): Promise<void> {
  if (text.trim()) return pairFromText(text, "paste");
  const read = await readClipboard();
  if (read.kind === "refused") return pairingIssue.set(pasteRefused(read.error));
  if (read.kind === "empty") return pairingIssue.set(LINK_REQUIRED);
  await pairFromText(read.text, "paste");
}

/** "Pick QR image": the code in a screenshot or photo the person picks (the host reads it). */
export async function pairFromPicture(): Promise<void> {
  pairingIssue.set(null);
  let picked: { value?: string | null } | null;
  try {
    picked = (await host.call("scan.image")) as { value?: string | null } | null;
  } catch (error) {
    return pairingIssue.set({ title: "Couldn't read that picture", detail: error instanceof Error ? error.message : String(error) });
  }
  if (!picked) return; // nothing picked
  if (!picked.value) return pairingIssue.set(NO_CODE_IN_PICTURE);
  await pairFromText(picked.value, "paste");
}
