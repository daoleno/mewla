import type { KeyboardForegroundSnapshot } from "keyboard-lifecycle";
import { keyboardOverlap, type KeyboardFrame } from "./terminalKeyboardInset";

/**
 * Interface mode's keyboard fact on iOS, from the same source as the terminal's
 * key bar: the frame the keyboard events report, clipped to the window
 * (`keyboardOverlap`).
 *
 * The native snapshot's `window.keyboardLayoutGuide.layoutFrame` is not that
 * fact. Nothing constrains the guide, so it reads "no keyboard" with a real
 * 329pt keyboard up (Mac log, iPhone 18 Pro): the Composer gate never opened
 * and the keyboard covered the Composer. The snapshot keeps owning which view
 * is focused; whether and how far the keyboard covers the window comes from
 * the frame.
 */
export type IosKeyboardFrameEventName =
  | "keyboardWillShow"
  | "keyboardDidShow"
  | "keyboardWillChangeFrame"
  | "keyboardDidChangeFrame"
  | "keyboardWillHide"
  | "keyboardDidHide";

export interface IosKeyboardFrameUpdate {
  /** The latest frame; null when the keyboard is down or covers nothing. */
  frame: KeyboardFrame | null;
  /**
   * Whether the native fact should be re-read now. A frame that leaves the
   * window is recorded without a re-read: the keyboard is still animating out,
   * and the Composer must travel down with it. The hide's own end sample (or
   * keyboardDidHide) re-reads it.
   */
  reread: boolean;
}

export function nextIosKeyboardFrame(
  name: IosKeyboardFrameEventName,
  frame: KeyboardFrame | null,
  windowHeight: number,
): IosKeyboardFrameUpdate {
  if (name === "keyboardDidHide") return { frame: null, reread: true };
  if (name === "keyboardWillHide") return { frame: null, reread: false };
  const covers = keyboardOverlap(windowHeight, frame) > 0;
  if (!covers) {
    // A will-change that leaves the window is the start of a hide; the did
    // variant is its end.
    const settled = name === "keyboardDidShow" || name === "keyboardDidChangeFrame";
    return { frame: null, reread: settled };
  }
  return { frame, reread: true };
}

/** The native snapshot with the keyboard's cover taken from the frame (iOS). */
export function snapshotWithKeyboardFrame(
  snapshot: KeyboardForegroundSnapshot,
  windowHeight: number,
  frame: KeyboardFrame | null,
): KeyboardForegroundSnapshot {
  const overlap = keyboardOverlap(windowHeight, frame);
  return {
    ...snapshot,
    imeVisible: overlap > 0,
    imeHeight: overlap,
    evidence: `${snapshot.evidence}+keyboard_frame`,
  };
}
