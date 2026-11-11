import { describe, expect, test } from "bun:test";
import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { coalesce, opaque, parseWebTerminalEvent, terminalPageHtml, webCommand, webTheme, type WebTerminalCommand } from "./webSurface";

const PAGE_DIR = join(import.meta.dir, "../../ui/web/terminal");
// The page's own helpers: terminal.js exports them where `module` exists (here), and runs where it does not (the page).
const page = require(join(PAGE_DIR, "terminal.js")) as {
  keyAction(event: { type: string; key: string; ctrlKey: boolean; metaKey: boolean; shiftKey: boolean; altKey: boolean }, hasSelection: boolean, mac: boolean): { kind: string; clear: boolean };
  utf16Index(cells: [string, number][], length: number): number;
  gather(pending: number, px: number, unit: number): { pending: number; lines: number };
  viewportOf(scrollTop: number, scrollMax: number, rowHeight: number, historyRows: number): { top: number; atBottom: boolean };
  osc52Text(data: string): string | null;
};

const PALETTE = ["#000000", "#110000", "#001100", "#111100", "#000011", "#110011", "#001111", "#cccccc", "#555555", "#ff0000", "#00ff00", "#ffff00", "#0000ff", "#ff00ff", "#00ffff", "#ffffff"];

describe("webTheme", () => {
  test("xterm's theme from the app's: opaque colors, the selection keeps its alpha, the cursor's text is the background", () => {
    const theme = webTheme({ background: "#fbfaf7ff", foreground: "#161412", cursor: "#161412ff", selection: "#2C55C055", palette: PALETTE });
    expect(theme.background).toBe("#fbfaf7");
    expect(theme.cursor).toBe("#161412");
    expect(theme.cursorAccent).toBe("#fbfaf7");
    expect(theme.selectionBackground).toBe("#2C55C055");
    expect(theme.black).toBe("#000000");
    expect(theme.white).toBe("#cccccc");
    expect(theme.brightRed).toBe("#ff0000");
    expect(theme.brightWhite).toBe("#ffffff");
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

  test("history rows arrive styled as spans", () => {
    expect(webCommand("history", { reset: false, removed: 2, rows: ["\u001b[31mred", "plain"], cols: 80, alternate: false })).toEqual({
      type: "history", reset: false, removed: 2, rows: [[{ t: "red", c: "var(--c1)" }], [{ t: "plain" }]], cols: 80, alternate: false,
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
  test("no inline script: the page's scripts and xterm's styles are files at the origin's /terminal/", () => {
    const html = terminalPageHtml();
    expect(html).toContain('<script data-xterm src="/terminal/xterm.js"></script>');
    expect(html).toContain('<script src="/terminal/terminal.js"></script>');
    expect(html).toContain('<link rel="stylesheet" href="/terminal/xterm.css">');
    expect(html.match(/<script(\s[^>]*)?>/g)!.every((tag) => / src="/.test(tag))).toBe(true);
    expect(html.indexOf("xterm.js")).toBeLessThan(html.indexOf("terminal.js"));
  });
});

describe("the page's helpers (terminal.js)", () => {
  const key = (k: string, mods: Partial<{ ctrl: boolean; meta: boolean; shift: boolean; alt: boolean }> = {}) =>
    ({ type: "keydown", key: k, ctrlKey: !!mods.ctrl, metaKey: !!mods.meta, shiftKey: !!mods.shift, altKey: !!mods.alt });

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

  test("a cell's UTF-16 index: empty cells are spaces, a wide character's right half is that character", () => {
    expect(page.utf16Index([["a", 1], ["b", 1], ["c", 1]], 3)).toBe(2);
    expect(page.utf16Index([["中", 2], ["", 0]], 1)).toBe(0);
    expect(page.utf16Index([["中", 2], ["", 0], ["x", 1]], 2)).toBe(1);
    expect(page.utf16Index([["🐱", 2], ["", 0], ["x", 1]], 3)).toBe(2);
    expect(page.utf16Index([["a", 1], ["", 1], ["b", 1]], 3)).toBe(2);
    expect(page.utf16Index([["a", 1], ["", 1]], 1)).toBe(-1);
  });

  test("drag pixels become whole lines and keep the rest", () => {
    expect(page.gather(0, 100, 40)).toEqual({ pending: 20, lines: 2 });
    expect(page.gather(20, 30, 40)).toEqual({ pending: 10, lines: 1 });
    expect(page.gather(0, -50, 40)).toEqual({ pending: -10, lines: -1 });
  });

  test("the viewport: the row at the top, the history's length at the bottom", () => {
    expect(page.viewportOf(320, 320, 16, 20)).toEqual({ top: 20, atBottom: true });
    expect(page.viewportOf(40, 320, 16, 20)).toEqual({ top: 2, atBottom: false });
    expect(page.viewportOf(0, 0, 16, 0)).toEqual({ top: 0, atBottom: true });
  });

  test("OSC 52: the clipboard's text; queries and X11 selections are not copies", () => {
    expect(page.osc52Text(`c;${btoa("hello")}`)).toBe("hello");
    expect(page.osc52Text(`;${Buffer.from("中文").toString("base64")}`)).toBe("中文");
    expect(page.osc52Text("c;?")).toBeNull();
    expect(page.osc52Text(`p;${btoa("x")}`)).toBeNull();
    expect(page.osc52Text("c;%%%")).toBeNull();
  });
});

describe("vendor.json", () => {
  test("names the vendored files beside it with the digests it records, and their license", () => {
    const vendor = JSON.parse(readFileSync(join(PAGE_DIR, "vendor.json"), "utf8")) as { files: Record<string, { sha256: string; licenseFile: string }> };
    for (const [name, file] of Object.entries(vendor.files)) {
      const digest = createHash("sha256").update(readFileSync(join(PAGE_DIR, name))).digest("hex");
      expect({ name, digest }).toEqual({ name, digest: file.sha256 });
      expect(readFileSync(join(PAGE_DIR, file.licenseFile), "utf8")).toContain("MIT");
    }
  });
});
