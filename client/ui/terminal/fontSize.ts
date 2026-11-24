import { platform, signal, Storage } from "nsh";
import { DESKTOP_TERMINAL_FONT, getTerminalFontSize, setTerminalFontSize, TERMINAL_FONT, terminalFontSize } from "../../core/sessions/deviceState";

/** The web's larger screens draw at 13, with no pinch. */
const WEB_FONT_SIZE = 13;

/** This device's sizes: the phones' pinch (8–14), or the Mac's ⌘+ and ⌘− (9–24, 13 by default). */
export const TERMINAL_TEXT = platform.os === "macos" ? DESKTOP_TERMINAL_FONT : TERMINAL_FONT;

/**
 * The terminal's text size on this device. The phones' and the Mac's is remembered (a pinch, or ⌘+ / ⌘−; SPEC 11,
 * Terminal) and is undefined until it is read, once per run, before a terminal reports its first grid.
 */
export const terminalTextSize = signal<number | undefined>(platform.os === "web" ? WEB_FONT_SIZE : undefined);

let reading = false;

/** Reads the remembered size once; a failed read is the default. */
export function loadTerminalTextSize(): void {
  if (reading || terminalTextSize() !== undefined) return;
  reading = true;
  void getTerminalFontSize(Storage, TERMINAL_TEXT)
    .catch(() => TERMINAL_TEXT.default)
    .then((size) => {
      if (terminalTextSize() === undefined) terminalTextSize.set(size);
    });
}

/** A pinch ended at `size` (or ⌘+ / ⌘− asked for it): every terminal on this device draws at it from now on. */
export function setTerminalTextSize(size: number): void {
  const next = terminalFontSize(size, TERMINAL_TEXT);
  terminalTextSize.set(next);
  void setTerminalFontSize(Storage, next, TERMINAL_TEXT).catch(() => {});
}

/** The Mac's View menu: one size bigger (1), smaller (-1), or the default (0). */
export function stepTerminalTextSize(step: -1 | 0 | 1): void {
  setTerminalTextSize(step === 0 ? TERMINAL_TEXT.default : (terminalTextSize() ?? TERMINAL_TEXT.default) + step);
}
