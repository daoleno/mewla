import type { HistoryRender } from "./history";
import { ansiSpans, type WebSpan } from "./webAnsi";

// The browser's terminal: the page client/ui/web/terminal/terminal.js runs xterm.js
// in a web view (an iframe sandboxed to an origin of its own), and
// client/ui/terminal/web/WebTerminalSurface.tsx speaks the terminal view's
// contract (client/ui/terminal/contract.ts) for it. What passes between them:
// the app posts batches of commands (`postMessage`, a JSON array); the page
// answers one event at a time over the web view's message port.
//
// The page's scripts are files the computer serves beside the app
// (`/terminal/`), not inline: the page inherits the app page's
// Content-Security-Policy, which runs no inline script.

/** The view's colors (contract.ts TerminalTheme): CSS hex, `#rrggbb` or `#rrggbbaa`. */
export type WebTerminalThemeInput = { background: string; foreground: string; cursor: string; selection: string; palette: string[] };

/** xterm.js's ITheme, as the page takes it. */
export type WebTerminalTheme = {
  background: string;
  foreground: string;
  cursor: string;
  cursorAccent: string;
  selectionBackground: string;
  black: string; red: string; green: string; yellow: string; blue: string; magenta: string; cyan: string; white: string;
  brightBlack: string; brightRed: string; brightGreen: string; brightYellow: string; brightBlue: string; brightMagenta: string; brightCyan: string; brightWhite: string;
};

export const ANSI_NAMES = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
  "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite"] as const;

/** Latin monospace faces first: xterm sizes its cells from the first family it finds, and Linux's `monospace` can be a CJK face. */
export const WEB_TERMINAL_FONT = '"SF Mono", Menlo, Monaco, "Cascadia Mono", Consolas, "DejaVu Sans Mono", "Liberation Mono", "Noto Sans Mono", monospace';

/** `#rrggbbaa` without its alpha (the VT draws opaque colors, as on the phones). */
export function opaque(color: string): string {
  return /^#[0-9a-f]{8}$/i.test(color) ? color.slice(0, 7) : color;
}

export function webTheme(theme: WebTerminalThemeInput): WebTerminalTheme {
  const background = opaque(theme.background);
  const palette = Object.fromEntries(ANSI_NAMES.map((name, i) => [name, opaque(theme.palette[i] ?? theme.foreground)]));
  return {
    background,
    foreground: opaque(theme.foreground),
    cursor: opaque(theme.cursor),
    cursorAccent: background,
    // The selection keeps its alpha: the text shows through it.
    selectionBackground: theme.selection,
    ...palette,
  } as WebTerminalTheme;
}

/** What the app tells the page. */
export type WebTerminalCommand =
  | { type: "theme"; theme: WebTerminalTheme }
  | { type: "font"; size: number; family: string }
  | { type: "write"; data: string }
  | { type: "reset" }
  | { type: "focus" }
  | { type: "blur" }
  | { type: "history"; reset: boolean; removed: number; rows: WebSpan[][]; cols: number; alternate: boolean }
  | { type: "scrollTo"; row: number }
  | { type: "scrollToBottom" }
  | { type: "mark"; row: number }
  | { type: "retry" }
  /** For checks: the page answers with what its screen holds (`probe`). */
  | { type: "probe" };

/** What the page tells the app: the contract's events, and the page's own. */
export type WebTerminalEvent =
  /** The page runs (again: a reload starts empty). `touch`: the device has a touch pointer. */
  | { type: "ready"; touch: boolean }
  | { type: "pointer"; touch: boolean }
  | { type: "size"; cols: number; rows: number }
  | { type: "failed"; message: string }
  | { type: "input"; data: string }
  | { type: "scroll"; lines: number }
  | { type: "viewport"; top: number; atBottom: boolean }
  | { type: "tapCell"; col: number; row: number; text: string; index: number }
  | { type: "copy"; text: string }
  /** `text`: the screen's rows as the VT holds them; `drawn`: as the page drew them; `history`: the rows above; `grid`: what the page measured. */
  | { type: "probe"; text: string; drawn: string; history: number; grid: string };

/** A program's copy (OSC 52) larger than this is not a copy. */
export const MAX_COPY_CHARS = 1 << 20;

const int = (value: unknown): value is number => typeof value === "number" && Number.isInteger(value);
const str = (value: unknown, max = MAX_COPY_CHARS): value is string => typeof value === "string" && value.length <= max;

/** Parses a page event; anything unexpected is dropped. */
export function parseWebTerminalEvent(raw: unknown): WebTerminalEvent | null {
  if (typeof raw !== "string" || raw.length > 4 * MAX_COPY_CHARS) return null;
  let value: Record<string, unknown>;
  try {
    value = JSON.parse(raw);
  } catch {
    return null;
  }
  if (!value || typeof value !== "object") return null;
  switch (value.type) {
    case "ready":
    case "pointer":
      return typeof value.touch === "boolean" ? { type: value.type, touch: value.touch } : null;
    case "size":
      return int(value.cols) && int(value.rows) && value.cols > 0 && value.rows > 0 ? { type: "size", cols: value.cols, rows: value.rows } : null;
    case "failed":
      return str(value.message, 2000) ? { type: "failed", message: value.message } : null;
    case "input":
      return str(value.data) && value.data.length > 0 ? { type: "input", data: value.data } : null;
    case "scroll":
      return int(value.lines) && value.lines !== 0 ? { type: "scroll", lines: value.lines } : null;
    case "viewport":
      return int(value.top) && value.top >= 0 && typeof value.atBottom === "boolean" ? { type: "viewport", top: value.top, atBottom: value.atBottom } : null;
    case "tapCell":
      return int(value.col) && int(value.row) && str(value.text, 100_000) && int(value.index)
        ? { type: "tapCell", col: value.col, row: value.row, text: value.text, index: value.index } : null;
    case "copy":
      return str(value.text) && value.text.length > 0 ? { type: "copy", text: value.text } : null;
    case "probe":
      return str(value.text) && str(value.drawn) && int(value.history) && str(value.grid, 200)
        ? { type: "probe", text: value.text, drawn: value.drawn, history: value.history, grid: value.grid } : null;
    default:
      return null;
  }
}

const row = (args: unknown): number | null => {
  const value = (args as { row?: unknown } | null)?.row;
  return int(value) ? value : null;
};

/** The contract's command (`ref.command(name, args)`) as the page's; null for one it does not take. */
export function webCommand(name: string, args: unknown): WebTerminalCommand | null {
  switch (name) {
    case "write": {
      const data = (args as { data?: unknown } | null)?.data;
      return typeof data === "string" && data ? { type: "write", data } : null;
    }
    case "reset":
    case "focus":
    case "blur":
    case "scrollToBottom":
    case "retry":
    case "probe":
      return { type: name };
    case "scrollTo": {
      const r = row(args);
      return r === null ? null : { type: "scrollTo", row: Math.max(0, r) };
    }
    case "mark": {
      const r = row(args);
      return r === null ? null : { type: "mark", row: r };
    }
    case "history": {
      const render = args as HistoryRender | null;
      if (!render || !Array.isArray(render.rows)) return null;
      return {
        type: "history",
        reset: render.reset === true,
        removed: int(render.removed) ? Math.max(0, render.removed) : 0,
        rows: render.rows.map((ansi) => ansiSpans(String(ansi))),
        cols: int(render.cols) ? render.cols : 0,
        alternate: render.alternate === true,
      };
    }
    default:
      return null;
  }
}

/**
 * One batch for the page: consecutive writes as one, and of the theme and
 * the font only the last (a batch held while the page loads).
 */
export function coalesce(batch: readonly WebTerminalCommand[]): WebTerminalCommand[] {
  const lastTheme = batch.findLastIndex((m) => m.type === "theme");
  const lastFont = batch.findLastIndex((m) => m.type === "font");
  const out: WebTerminalCommand[] = [];
  batch.forEach((message, i) => {
    if ((message.type === "theme" && i !== lastTheme) || (message.type === "font" && i !== lastFont)) return;
    const last = out[out.length - 1];
    if (message.type === "write" && last?.type === "write") out[out.length - 1] = { type: "write", data: last.data + message.data };
    else out.push(message);
  });
  return out;
}

/**
 * Where the page's files are: at the root of the app's origin, beside the
 * app's own files (the computer serves the app there). Root-relative: an
 * `about:srcdoc` page resolves URLs against the app page's, which may be a
 * deep link, and a bundle's `import.meta.url` names its source, not its URL.
 */
export const TERMINAL_PAGE_DIR = "/terminal/";

const attr = (value: string) => value.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");

// Styles may be inline (the policy allows them); scripts may not.
const PAGE_STYLE = `html,body{margin:0;width:100%;height:100%;overflow:hidden;background:var(--bg,transparent);color:var(--fg,inherit)}
#scroller{position:fixed;inset:0;overflow:hidden;overscroll-behavior:none;touch-action:none}
#history>div{box-sizing:content-box;height:var(--cell-h,16px);line-height:var(--cell-h,16px);width:calc(var(--cols,80) * var(--cell-w,8px));padding-left:6px;white-space:pre;overflow:hidden;font-family:var(--font,monospace);font-size:var(--font-size,13px);cursor:text}
#history>div.mk{background:var(--sel)}
#screen{box-sizing:border-box;height:100%;padding:4px 0 0 6px}
.B{font-weight:bold}.D{opacity:.5}.I{font-style:italic}.U{text-decoration:underline}.S{text-decoration:line-through}.U.S{text-decoration:underline line-through}.O{text-decoration:overline}`;

/** The page, its files in `dir`. */
export function terminalPageHtml(dir = TERMINAL_PAGE_DIR): string {
  const url = (file: string) => attr(dir + file);
  return `<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1,user-scalable=no">
<link rel="stylesheet" href="${url("xterm.css")}">
<style>${PAGE_STYLE}</style>
</head><body><div id="scroller"><div id="history"></div><div id="screen"></div></div>
<script data-xterm src="${url("xterm.js")}"></script>
<script src="${url("terminal.js")}"></script>
</body></html>`;
}
