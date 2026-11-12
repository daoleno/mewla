import { describe, expect, test } from "bun:test";
import { join } from "node:path";
import { coalesce, opaque, parseWebTerminalEvent, terminalPageHtml, webCommand, webTheme, type WebTerminalCommand } from "./webSurface";

const PAGE_DIR = join(import.meta.dir, "../../ui/web/terminal");
type Run = { col: number; cells: number; flags: number; text: string };
type Line = { runs: Run[]; wrapped: boolean };
type KeyEvent = { type: string; key: string; code?: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean; getModifierState?(name: string): boolean };
// The page's own helpers: terminal.js exports them where `module` exists (here), and runs where it does not (the page).
const page = require(join(PAGE_DIR, "terminal.js")) as {
  keyAction(event: KeyEvent, hasSelection: boolean, mac: boolean): { kind: string; clear: boolean };
  keyCode(code: string): number;
  KEY_NAMES: string[];
  unshiftedOf(code: string): number;
  modsOf(event: KeyEvent, mac: boolean): number;
  keyText(event: KeyEvent): string;
  gather(pending: number, px: number, unit: number): { pending: number; lines: number };
  cellsText(line: Line, c0: number, c1: number): string;
  selectionText(lineAt: (i: number) => Line, from: [number, number], to: [number, number], cols: number): string;
  wordAt(line: Line, col: number, cols: number): [number, number] | null;
  lineSpan(lineAt: (i: number) => Line, i: number, count: number): [number, number];
  textIndex(line: Line, col: number): number;
  viewportOf(scrollPx: number, cellH: number, historyRows: number): { top: number; atBottom: boolean };
  palette256(sixteen: number[]): number[];
  rgbOf(color: string): number;
};

const PALETTE = ["#000000", "#110000", "#001100", "#111100", "#000011", "#110011", "#001111", "#cccccc", "#555555", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff"];
const CLUSTER = 128;

describe("webTheme", () => {
  test("the page's colors from the app's: opaque, the selection keeps its alpha, the palette's first 16", () => {
    const theme = webTheme({ background: "#fbfaf7ff", foreground: "#161412", cursor: "#161412ff", selection: "#2C55C055", palette: [...PALETTE, "#123456"] });
    expect(theme).toEqual({ background: "#fbfaf7", foreground: "#161412", cursor: "#161412", selection: "#2C55C055", palette: PALETTE });
    expect(webTheme({ background: "#000000", foreground: "#eeeeee", cursor: "#eeeeee", selection: "#ffffff33", palette: [] }).palette).toEqual(Array(16).fill("#eeeeee"));
    expect(opaque("#abc")).toBe("#abc");
  });
});

describe("webCommand", () => {
  test("the contract's commands as the page's; unknown or malformed ones are dropped", () => {
    expect(webCommand("write", { data: "hi" })).toEqual({ type: "write", data: "hi" });
    expect(webCommand("write", { data: "" })).toBeNull();
    expect(webCommand("reset", {})).toEqual({ type: "reset" });
    expect(webCommand("scrollTo", { row: -3 })).toEqual({ type: "scrollTo", row: 0 });
    expect(webCommand("mark", { row: -1 })).toEqual({ type: "mark", row: -1 });
    expect(webCommand("mark", { row: "2" })).toBeNull();
    expect(webCommand("zoomTo", {})).toBeNull();
  });

  test("history rows arrive as the computer keeps them (the page draws them through its VT)", () => {
    expect(webCommand("history", { reset: false, removed: 2, rows: ["\u001b[31mred", "plain"], cols: 80, alternate: false })).toEqual({
      type: "history", reset: false, removed: 2, rows: ["\u001b[31mred", "plain"], cols: 80, alternate: false,
    });
  });
});

describe("coalesce", () => {
  test("writes in a row become one, in order with the rest; only the last theme and font", () => {
    const theme = (bg: string): WebTerminalCommand => ({ type: "theme", theme: webTheme({ background: bg, foreground: "#000000", cursor: "#000000", selection: "#00000055", palette: PALETTE }) });
    const batch: WebTerminalCommand[] = [theme("#111111"), { type: "write", data: "a" }, { type: "write", data: "b" }, { type: "reset" }, { type: "write", data: "c" }, theme("#222222")];
    expect(coalesce(batch)).toEqual([{ type: "write", data: "ab" }, { type: "reset" }, { type: "write", data: "c" }, theme("#222222")]);
  });
});

describe("parseWebTerminalEvent", () => {
  test("takes the page's events and drops the rest", () => {
    expect(parseWebTerminalEvent(JSON.stringify({ type: "size", cols: 80, rows: 24 }))).toEqual({ type: "size", cols: 80, rows: 24 });
    expect(parseWebTerminalEvent(JSON.stringify({ type: "size", cols: 0, rows: 24 }))).toBeNull();
    expect(parseWebTerminalEvent(JSON.stringify({ type: "input", data: "\r" }))).toEqual({ type: "input", data: "\r" });
    expect(parseWebTerminalEvent(JSON.stringify({ type: "scroll", lines: 3 }))).toEqual({ type: "scroll", lines: 3 });
    expect(parseWebTerminalEvent(JSON.stringify({ type: "viewport", top: 4, atBottom: false }))).toEqual({ type: "viewport", top: 4, atBottom: false });
    expect(parseWebTerminalEvent(JSON.stringify({ type: "tapCell", col: -1, row: -1, text: "https://x.dev", index: 0, extra: 1 }))).toEqual({ type: "tapCell", col: -1, row: -1, text: "https://x.dev", index: 0 });
    expect(parseWebTerminalEvent(JSON.stringify({ type: "ready", touch: true }))).toEqual({ type: "ready", touch: true });
    expect(parseWebTerminalEvent(JSON.stringify({ type: "copy", text: "" }))).toBeNull();
    expect(parseWebTerminalEvent(JSON.stringify({ type: "eval", code: "x" }))).toBeNull();
    expect(parseWebTerminalEvent("not json")).toBeNull();
    expect(parseWebTerminalEvent(42)).toBeNull();
  });
});

describe("terminalPageHtml", () => {
  test("no inline script: the page's script is a file at the origin's /terminal/; a canvas and the input's textarea", () => {
    const html = terminalPageHtml();
    expect(html).toContain('<script src="/terminal/terminal.js"></script>');
    expect(html.match(/<script(\s[^>]*)?>/g)).toEqual(['<script src="/terminal/terminal.js">']);
    expect(html).toContain('<canvas id="vt"></canvas>');
    expect(html).toContain('<textarea id="input"');
    expect(html).not.toContain("xterm");
    // The phones' faces, served beside the page: the real italics too.
    for (const file of ["Regular", "SemiBold", "Italic", "SemiBoldItalic"]) expect(html).toContain(`src:url("/terminal/MapleMono-NF-${file}.ttf")`);
    expect(html).toContain("font-style:italic");
  });
});

describe("the page's helpers (terminal.js)", () => {
  const key = (k: string, mods: Partial<{ ctrl: boolean; meta: boolean; shift: boolean; alt: boolean }> = {}, code = ""): KeyEvent =>
    ({ type: "keydown", key: k, code, ctrlKey: !!mods.ctrl, metaKey: !!mods.meta, shiftKey: !!mods.shift, altKey: !!mods.alt });

  test("Mac: ⌘C copies a selection; ⌃C is the program's", () => {
    expect(page.keyAction(key("c", { meta: true }), true, true)).toEqual({ kind: "native", clear: false });
    expect(page.keyAction(key("c", { meta: true }), false, true).kind).toBe("pass");
    expect(page.keyAction(key("c", { ctrl: true }), true, true).kind).toBe("pass");
    expect(page.keyAction(key("v", { meta: true }), false, true).kind).toBe("pass");
  });

  test("elsewhere: Ctrl+Shift+C copies or is swallowed, Ctrl+C copies only a selection (dropping it), Ctrl+Shift+V pastes", () => {
    expect(page.keyAction(key("C", { ctrl: true, shift: true }), true, false)).toEqual({ kind: "copy", clear: false });
    expect(page.keyAction(key("C", { ctrl: true, shift: true }), false, false).kind).toBe("swallow");
    expect(page.keyAction(key("c", { ctrl: true }), true, false)).toEqual({ kind: "native", clear: true });
    expect(page.keyAction(key("c", { ctrl: true }), false, false).kind).toBe("pass");
    expect(page.keyAction(key("V", { ctrl: true, shift: true }), false, false).kind).toBe("native");
    expect(page.keyAction(key("v", { ctrl: true }), false, false).kind).toBe("pass");
    expect(page.keyAction(key("c", { ctrl: true, alt: true }), true, false).kind).toBe("pass");
    expect(page.keyAction({ ...key("c", { ctrl: true }), type: "keyup" }, true, false).kind).toBe("pass");
  });

  test("keys: KeyboardEvent.code as libghostty-vt's key, the text a key types, its modifiers", () => {
    expect(page.keyCode("KeyA")).toBe(20);
    expect(page.keyCode("Enter")).toBe(58);
    expect(page.keyCode("ArrowUp")).toBe(78);
    expect(page.keyCode("F1")).toBe(121);
    expect(page.keyCode("Paste")).toBe(175);
    expect(page.keyCode("Nope")).toBe(0);
    expect(page.KEY_NAMES.length).toBe(175);
    expect(page.unshiftedOf("KeyQ")).toBe("q".charCodeAt(0));
    expect(page.unshiftedOf("Digit7")).toBe("7".charCodeAt(0));
    expect(page.unshiftedOf("Enter")).toBe(0);
    expect(page.keyText(key("a"))).toBe("a");
    expect(page.keyText(key("Enter"))).toBe("");
    expect(page.keyText(key("😀"))).toBe("😀");
    expect(page.modsOf(key("c", { ctrl: true, shift: true }), false)).toBe(1 | 2);
    // A Mac's Option that typed a character is that character, not Alt; elsewhere Alt stays.
    expect(page.modsOf(key("∫", { alt: true }), true)).toBe(0);
    expect(page.modsOf(key("b", { alt: true }), false)).toBe(4);
    // AltGr (Ctrl+Alt on Windows) types its character.
    expect(page.modsOf({ ...key("@", { ctrl: true, alt: true }), getModifierState: (m: string) => m === "AltGraph" }, false)).toBe(0);
  });

  test("drag pixels become whole lines and keep the rest", () => {
    expect(page.gather(0, 100, 40)).toEqual({ pending: 20, lines: 2 });
    expect(page.gather(20, 30, 40)).toEqual({ pending: 10, lines: 1 });
    expect(page.gather(0, -50, 40)).toEqual({ pending: -10, lines: -1 });
  });

  test("the viewport: the history row at the top, the history's length at the bottom", () => {
    expect(page.viewportOf(0, 16, 20)).toEqual({ top: 20, atBottom: true });
    expect(page.viewportOf(16 * 18, 16, 20)).toEqual({ top: 2, atBottom: false });
    expect(page.viewportOf(8, 16, 20)).toEqual({ top: 19, atBottom: false });
    expect(page.viewportOf(0, 16, 0)).toEqual({ top: 0, atBottom: true });
  });

  // The phones' selection rules (TerminalView.kt), on the packer's runs.
  const plain = (col: number, text: string): Run => ({ col, cells: text.length, flags: 0, text });
  const wide = (col: number, text: string): Run => ({ col, cells: 2, flags: CLUSTER, text });
  const lines: Line[] = [
    { runs: [plain(0, "$ echo hello world")], wrapped: false },
    { runs: [plain(0, "hello"), plain(6, "world")], wrapped: false },
    { runs: [plain(0, "abc"), wide(3, "中"), plain(5, "x")], wrapped: true },
    { runs: [plain(0, "def")], wrapped: false },
    { runs: [], wrapped: false },
  ];
  const at = (i: number) => lines[i];

  test("cells as text: blanks between runs are spaces, a wide character once", () => {
    expect(page.cellsText(lines[1], 0, 10)).toBe("hello world");
    expect(page.cellsText(lines[1], 2, 7)).toBe("llo wo");
    expect(page.cellsText(lines[2], 0, 5)).toBe("abc中x");
  });

  test("selected text: rows joined by newlines, a wrapped row runs on, trailing blanks and empty rows dropped", () => {
    expect(page.selectionText(at, [0, 2], [1, 4], 20)).toBe("echo hello world\nhello");
    expect(page.selectionText(at, [2, 0], [3, 19], 20)).toBe("abc中xdef");
    expect(page.selectionText(at, [3, 0], [4, 19], 20)).toBe("def");
  });

  test("a word: the run of non-blank cells around a cell; none on a blank", () => {
    expect(page.wordAt(lines[1], 8, 20)).toEqual([6, 10]);
    expect(page.wordAt(lines[1], 5, 20)).toBeNull();
    expect(page.wordAt(lines[2], 4, 20)).toEqual([0, 5]);
  });

  test("a line: the rows a wrapped line spans", () => {
    expect(page.lineSpan(at, 3, lines.length)).toEqual([2, 3]);
    expect(page.lineSpan(at, 2, lines.length)).toEqual([2, 3]);
    expect(page.lineSpan(at, 1, lines.length)).toEqual([1, 1]);
  });

  test("a cell's UTF-16 index: blanks are spaces, a wide character's right half is that character, -1 past the end", () => {
    expect(page.textIndex(lines[1], 6)).toBe(6);
    expect(page.textIndex(lines[1], 5)).toBe(5);
    expect(page.textIndex(lines[2], 4)).toBe(3);
    expect(page.textIndex(lines[2], 5)).toBe(4);
    expect(page.textIndex(lines[1], 15)).toBe(-1);
  });

  test("the 256 colors: the theme's 16, the cube, the grays", () => {
    const colors = page.palette256(PALETTE.map(page.rgbOf));
    expect(colors.length).toBe(256);
    expect(colors[9]).toBe(0xff0000);
    expect(colors[16]).toBe(0);
    expect(colors[231]).toBe(0xffffff);
    expect(colors[232]).toBe(0x080808);
    expect(page.rgbOf("#fbfaf7ff")).toBe(0xfbfaf7);
  });
});
