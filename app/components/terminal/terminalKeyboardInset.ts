/**
 * How far the terminal's key bar sits above the bottom of the window.
 *
 * Pure so a rotation or a fold can be tested without a device. The hook feeds
 * it the window as it is now and the keyboard frame as last reported.
 */

export interface KeyboardFrame {
  /** Top edge of the keyboard in screen coordinates. */
  screenY: number;
  height: number;
}

/**
 * The part of the window the keyboard really covers (iOS).
 *
 * A keyboard event's `height` is not that: with a hardware keyboard, a
 * floating keyboard, or after a rotation the frame can be non-empty while its
 * top edge is at or below the window's bottom, so nothing is covered.
 */
export function keyboardOverlap(
  windowHeight: number,
  frame: KeyboardFrame | null,
): number {
  if (!frame || !(frame.height > 0) || !(windowHeight > 0)) return 0;
  const covered = windowHeight - frame.screenY;
  if (!(covered > 0)) return 0;
  return Math.min(covered, frame.height);
}

/**
 * Android, edge-to-edge or not: the part of the keyboard the window's own
 * resize did not already give back. `baseWindowHeight` is the window's height
 * with no keyboard; unknown (null) means the resize is taken as handled.
 */
export function androidKeyboardInset(
  keyboardHeight: number,
  baseWindowHeight: number | null,
  windowHeight: number,
): number {
  if (!(keyboardHeight > 0) || baseWindowHeight === null) return 0;
  const handled = Math.max(0, baseWindowHeight - windowHeight);
  return Math.max(0, keyboardHeight - handled);
}

/**
 * The height of the window with no keyboard, for Android's resize maths.
 * It is only known while the keyboard is down, and only for the width it was
 * measured at: a rotation or a fold with the keyboard up changes the window
 * without a keyboard event, so the old height says nothing about the new one.
 */
export function nextAndroidBaseWindow(
  previous: { width: number; height: number } | null,
  window: { width: number; height: number },
  keyboardVisible: boolean,
): { width: number; height: number } | null {
  if (!keyboardVisible) return { width: window.width, height: window.height };
  if (previous && previous.width === window.width) return previous;
  return null;
}

/**
 * Whether the keyboard is up, for the key bar's toggle and its offset: one
 * fact for both. On iOS a show event is not enough, because a rotation or a
 * fold can leave a "shown" keyboard whose frame covers nothing (or dismiss it
 * without a hide event): it is up while its latest frame overlaps the window.
 * Android's resize-based frame has no such coordinates; its events decide.
 */
export function keyboardIsUp(
  platform: "ios" | "android" | string,
  shownByEvent: boolean,
  windowHeight: number,
  frame: KeyboardFrame | null,
): boolean {
  if (!shownByEvent) return false;
  if (platform === "ios") return keyboardOverlap(windowHeight, frame) > 0;
  return (frame?.height ?? 0) > 0;
}

export interface AccessoryOffsetInput {
  platform: "ios" | "android" | string;
  keyboardVisible: boolean;
  safeAreaBottom: number;
  windowHeight: number;
  keyboardFrame: KeyboardFrame | null;
  androidInset: number;
}

/** `bottom` of the absolutely positioned key bar. */
export function accessoryBottomOffset({
  platform,
  keyboardVisible,
  safeAreaBottom,
  windowHeight,
  keyboardFrame,
  androidInset,
}: AccessoryOffsetInput): number {
  if (!keyboardVisible) return safeAreaBottom;
  if (platform === "ios") {
    const overlap = keyboardOverlap(windowHeight, keyboardFrame);
    return overlap > 0 ? overlap : safeAreaBottom;
  }
  return androidInset;
}
