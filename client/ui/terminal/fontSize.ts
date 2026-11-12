import { platform, signal, Storage } from "nsh";
import { getTerminalFontSize, setTerminalFontSize, TERMINAL_FONT, terminalFontSize } from "../../core/sessions/deviceState";

/** The web's larger screens draw at 13, with no pinch. */
const WEB_FONT_SIZE = 13;

/**
 * The terminal's text size on this device. The phones' is remembered (a pinch sets 8–14, SPEC 11,
 * Terminal) and is undefined until it is read, once per run, before a terminal reports its first grid.
 */
export const terminalTextSize = signal<number | undefined>(platform.os === "web" ? WEB_FONT_SIZE : undefined);

let reading = false;

/** Reads the remembered size once; a failed read is the default. */
export function loadTerminalTextSize(): void {
  if (reading || terminalTextSize() !== undefined) return;
  reading = true;
  void getTerminalFontSize(Storage)
    .catch(() => TERMINAL_FONT.default)
    .then((size) => {
      if (terminalTextSize() === undefined) terminalTextSize.set(size);
    });
}

/** A pinch ended at `size`: every terminal on this device draws at it from now on. */
export function setTerminalTextSize(size: number): void {
  const next = terminalFontSize(size);
  terminalTextSize.set(next);
  void setTerminalFontSize(Storage, next).catch(() => {});
}
