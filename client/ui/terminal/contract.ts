import { nativeComponent } from "nsh";

/**
 * The terminal view: one contract, three renderers. Android (Kotlin, Canvas
 * text + InputConnection) and iOS (Swift, CoreText + UITextInput) are native
 * components holding libghostty-vt; the web has an NSH Canvas over ghostty's
 * wasm build (M5). The VT lives in the view: the page passes the computer's
 * output in (`write`) and sends what the view reports (`input`, `size`,
 * `scroll`, `tapCell`) to the computer. Selection and copy are the platform's
 * own (long press, system menu); IME composition happens in the view, and only
 * committed text is reported as `input`.
 */
/** Colors are CSS hex: `#rrggbb`, or `#rrggbbaa` (what `token()` gives; the VT drops the alpha). */
export type TerminalTheme = {
  background: string;
  foreground: string;
  cursor: string;
  selection: string;
  /** The 16 ANSI colors, `#rrggbb`. */
  palette: string[];
};

export type TerminalProps = {
  /** In points. The view derives its grid from it and its size. */
  fontSize: number;
  theme: TerminalTheme;
};

export type TerminalEvents = {
  /** The grid fits cols × rows now (first layout, rotation, keyboard, font size). */
  size: { cols: number; rows: number };
  /** Bytes for the program: typed text, keys (`\r`, `\x7f`, escape sequences), pasted text. */
  input: { data: string };
  /** A drag scrolled the history: `lines` > 0 is back in time. The page drives tmux copy mode with it. */
  scroll: { lines: number };
  /** A tap at a cell (tmux pane focus, mouse-mode programs). */
  tapCell: { col: number; row: number };
  /** A URL or file path under a tap. */
  link: { url: string };
};

/**
 * Commands (`ref.command(name, args)`):
 * - `write {data}`: output from the computer, in order.
 * - `reset {}`: clear the screen and history (a new attachment).
 * - `focus {}` / `blur {}`: show or hide the keyboard.
 * - `search {query, backwards}`: select the next match in the view's history.
 */
export const TerminalSurface = nativeComponent<TerminalProps, TerminalEvents>("mewla.terminal", {
  props: ["fontSize", "theme"],
  events: ["size", "input", "scroll", "tapCell", "link"],
});
