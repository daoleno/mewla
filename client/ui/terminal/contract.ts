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
 *
 * The history above the screen is not the VT's: the computer keeps it
 * (`terminal_history`, core/terminal/history.ts) and the page hands its rows
 * to the view (`history`), which draws them above the live screen and scrolls
 * through them on its own. Search and the prompt jumps run in the page over
 * the same rows and move the view (`scrollTo`, `mark`).
 */
/** Colors are CSS hex: `#rrggbb`, or `#rrggbbaa` (what `token()` gives; the VT drops the alpha). */
export type TerminalTheme = {
  background: string;
  foreground: string;
  cursor: string;
  selection: string;
  /** The selection handles' color (Android tints the platform's handle shapes with it). */
  handle?: string;
  /** The 16 ANSI colors, `#rrggbb`. */
  palette: string[];
};

export type TerminalProps = {
  /**
   * In points. The view derives its grid from it and its size; Android's line height is 1.4 times it in
   * whole points (10/14, 9/13). The page keeps the device's size (ui/terminal/fontSize.ts).
   */
  fontSize: number;
  theme: TerminalTheme;
  /**
   * In points: room the view keeps clear inside its edges (the terminal group's padding). The grid is
   * the area inside it; scrolled history and a panned grid still draw on into it, up to the view's edges.
   * Android draws it; iOS and the web view leave it unset and sit inside the padding instead.
   */
  padding?: { top: number; right: number; bottom: number; left: number };
};

export type TerminalEvents = {
  /** The grid fits cols × rows now (first layout, rotation, keyboard, font size). */
  size: { cols: number; rows: number };
  /** The VT could not be made (the library did not load, or refused the grid); `retry` tries again. */
  failed: { message: string };
  /** Bytes for the program: typed text, keys (`\r`, `\x7f`, escape sequences), pasted text. */
  input: { data: string };
  /**
   * A drag the view did not take, `lines` > 0 back in time: it holds no
   * history, or the program is on the alternate screen. The page drives tmux
   * copy mode with it.
   */
  scroll: { lines: number };
  /** Where the history viewport stands: `top` is the history row at the top (the history's length at the bottom). */
  viewport: { top: number; atBottom: boolean };
  /**
   * A tap. `row` is the live screen's row, -1 on a history row; `text` is
   * the tapped row's text and `index` the UTF-16 index of the tapped cell in
   * it (-1 past its end). The page opens a link there, or focuses. The view
   * does not raise the keyboard itself.
   */
  tapCell: { col: number; row: number; text: string; index: number };
  /** A program copied text (OSC 52, OSC 1337 Copy, OSC 5522). */
  copy: { text: string };
  /**
   * A pinch ended at `size` (whole points, 8–14). While the fingers move the view only scales its drawing;
   * the page sets the size as `fontSize`, which changes the grid once. A view drawing the Session's size
   * for another viewer (`sessionSize`) keeps its fit and takes no pinch. Android only so far.
   */
  fontSize: { size: number };
};

/**
 * Commands (`ref.command(name, args)`):
 * - `write {data}`: output from the computer, in order.
 * - `reset {}`: clear the screen (a new attachment). The history is the `history` command's.
 * - `sessionSize {cols, rows}`: the Session's size, before the replay drawn at it. When it is not the
 *   view's grid (another viewer holds the size), the VT takes it and the view draws that screen fitted
 *   into its area: scaled down to a readable limit, then panned sideways. Its `size` event still
 *   reports its own grid; the Session's size equal to it again ends the fit.
 * - `history {reset, removed, rows, cols, alternate}`: void the history rows (`reset`), drop
 *   `removed` from the front, append `rows` (styled ANSI, one per row, drawn `cols` wide).
 *   While scrolled up, the rows on screen stay put. `alternate`: drags go to the program (`scroll`).
 * - `scrollTo {row}`: show history row `row` at the top.
 * - `scrollToBottom {}`: back to the live screen.
 * - `mark {row}`: highlight one history row (the search's match); -1 none.
 * - `focus {}` / `blur {}`: show or hide the keyboard.
 * - `retry {}`: make the VT again after `failed`.
 * - `paste {text}`: text pasted into the program, framed for bracketed paste when the program asked for it
 *   (the key bar's Paste on the phones; the web pastes through the browser and has no such key).
 */
export const TerminalSurface = nativeComponent<TerminalProps, TerminalEvents>("mewla.terminal", {
  props: ["fontSize", "theme", "padding"],
  events: ["size", "failed", "input", "scroll", "viewport", "tapCell", "copy", "fontSize"],
});
