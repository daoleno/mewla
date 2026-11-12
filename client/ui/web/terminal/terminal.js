// The browser's terminal page: libghostty-vt with the phones' packer and glyphs
// (client/native/terminal, built to WebAssembly by scripts/build-libghostty-web.sh as
// mewla-vt.wasm beside this file), drawn on a canvas on a grid of whole device pixels, as
// the phones draw (TerminalView.kt, TerminalView.swift). The app builds the page
// (client/core/terminal/webSurface.ts terminalPageHtml) and speaks the terminal view's
// contract for it (client/ui/terminal/web/WebTerminalSurface.tsx).
//
// It runs in NSH's web view, an iframe sandboxed to an origin of its own whose document
// inherits the app page's Content-Security-Policy: no inline script, no eval, classic
// scripts from the app's origin only, WebAssembly allowed ('wasm-unsafe-eval'). So this is
// a plain classic script; its VT and faces come from the app's origin, which serves them
// to any origin (the frame's is opaque). It reaches the app over the message port NSH
// hands the page on load (NSH's own `window.nsh` bridge is an inline script, which that
// policy blocks; where it runs, it is used instead).
//
// Layout: one canvas. The history the computer keeps (whole lines, drawn through a second
// VT) sits above the screen; wheel and touch drags scroll it by the pixel, and with none,
// or with the program on the alternate screen, they go to the app as `scroll` lines. A
// hidden textarea at the cursor takes the keyboard and the input method: keys are encoded
// by libghostty-vt for the program's modes, a composition is drawn at the cursor and sent
// once it is committed.

(function () {
  "use strict";

  /** Wheel pixels per line (a notch is about three). */
  var WHEEL_LINE_PX = 40;
  /** How far a press may move and still be a tap. */
  var TAP_SLOP_PX = 4;
  var MAX_COPY_CHARS = 1 << 20;
  /** History rows drawn through the second VT at a time. */
  var PAGE = 64;
  /** The screen's inset in CSS pixels (left, top), as the page had it. */
  var PAD_X = 6, PAD_Y = 4;

  var FLAG = { BOLD: 1, ITALIC: 2, FAINT: 4, INVERSE: 8, STRIKE: 16, OVERLINE: 32, BLINK: 64, CLUSTER: 128,
    EMOJI_WIDE: 1 << 11, FLAG_HEAD: 1 << 12, FLAG_TAIL: 1 << 13 };
  var UNDERLINE_SHIFT = 8;
  var HEADER = 10, ROW = 3, RUN = 8;
  var MODS = { SHIFT: 1, CTRL: 2, ALT: 4, SUPER: 8, CAPS: 16, NUM: 32 };
  var KEY_PRESS = 1, KEY_REPEAT = 2;
  var MOUSE = { PRESS: 0, RELEASE: 1, MOTION: 2 };

  /**
   * libghostty-vt's GhosttyKey (ghostty/vt/key/event.h): the W3C KeyboardEvent.code names
   * in its order, from 1 (scripts/test-ghosttyvt.sh checks them against the pinned header).
   */
  var KEY_NAMES = ("Backquote Backslash BracketLeft BracketRight Comma Digit0 Digit1 Digit2 Digit3 Digit4 Digit5 " +
    "Digit6 Digit7 Digit8 Digit9 Equal IntlBackslash IntlRo IntlYen KeyA KeyB KeyC KeyD KeyE KeyF KeyG KeyH KeyI " +
    "KeyJ KeyK KeyL KeyM KeyN KeyO KeyP KeyQ KeyR KeyS KeyT KeyU KeyV KeyW KeyX KeyY KeyZ Minus Period Quote " +
    "Semicolon Slash AltLeft AltRight Backspace CapsLock ContextMenu ControlLeft ControlRight Enter MetaLeft " +
    "MetaRight ShiftLeft ShiftRight Space Tab Convert KanaMode NonConvert Delete End Help Home Insert PageDown " +
    "PageUp ArrowDown ArrowLeft ArrowRight ArrowUp NumLock Numpad0 Numpad1 Numpad2 Numpad3 Numpad4 Numpad5 " +
    "Numpad6 Numpad7 Numpad8 Numpad9 NumpadAdd NumpadBackspace NumpadClear NumpadClearEntry NumpadComma " +
    "NumpadDecimal NumpadDivide NumpadEnter NumpadEqual NumpadMemoryAdd NumpadMemoryClear NumpadMemoryRecall " +
    "NumpadMemoryStore NumpadMemorySubtract NumpadMultiply NumpadParenLeft NumpadParenRight NumpadSubtract " +
    "NumpadSeparator NumpadUp NumpadDown NumpadRight NumpadLeft NumpadBegin NumpadHome NumpadEnd NumpadInsert " +
    "NumpadDelete NumpadPageUp NumpadPageDown Escape F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12 F13 F14 F15 F16 F17 " +
    "F18 F19 F20 F21 F22 F23 F24 F25 Fn FnLock PrintScreen ScrollLock Pause BrowserBack BrowserFavorites " +
    "BrowserForward BrowserHome BrowserRefresh BrowserSearch BrowserStop Eject LaunchApp1 LaunchApp2 LaunchMail " +
    "MediaPlayPause MediaSelect MediaStop MediaTrackNext MediaTrackPrevious Power Sleep AudioVolumeDown " +
    "AudioVolumeMute AudioVolumeUp WakeUp Copy Cut Paste").split(" ");
  var KEY_CODES = {};
  for (var k = 0; k < KEY_NAMES.length; k++) KEY_CODES[KEY_NAMES[k]] = k + 1;

  /** A KeyboardEvent.code as libghostty-vt's key; 0 (unidentified) for one it does not know. */
  function keyCode(code) {
    return KEY_CODES[code] || 0;
  }

  /** The code point a key types without Shift (letters and digits; 0 when the layout does not say). */
  function unshiftedOf(code) {
    var m = /^Key([A-Z])$/.exec(code || "") || /^Digit([0-9])$/.exec(code || "");
    return m ? m[1].toLowerCase().charCodeAt(0) : 0;
  }

  /** A key event's modifiers as libghostty-vt's; on a Mac, Option that typed a character is that character's. */
  function modsOf(event, mac) {
    var mods = (event.shiftKey ? MODS.SHIFT : 0) | (event.ctrlKey ? MODS.CTRL : 0) | (event.altKey ? MODS.ALT : 0) | (event.metaKey ? MODS.SUPER : 0);
    var altGraph = event.getModifierState && event.getModifierState("AltGraph");
    if (altGraph) mods &= ~(MODS.CTRL | MODS.ALT);
    if (mac && event.altKey && typeof event.key === "string" && event.key.length === 1 && event.key.charCodeAt(0) > 0x7f) mods &= ~MODS.ALT;
    if (event.getModifierState && event.getModifierState("CapsLock")) mods |= MODS.CAPS;
    if (event.getModifierState && event.getModifierState("NumLock")) mods |= MODS.NUM;
    return mods;
  }

  /** The text a key types: its `key` when that is one character (a name such as "Enter" types none). */
  function keyText(event) {
    var key = event.key;
    if (typeof key !== "string" || key.length === 0) return "";
    return Array.from(key).length === 1 ? key : "";
  }

  /**
   * What a key press does with the clipboard: `native` lets the browser copy or paste (the
   * terminal stays out), `copy` copies the selection, `swallow` keeps the key from the program,
   * `pass` is the terminal's. ⌘C copies on a Mac; elsewhere Ctrl+Shift+C always belongs to the
   * terminal and plain Ctrl+C copies only while text is selected (so with nothing selected it
   * still interrupts), dropping the selection so the next one interrupts. Ctrl+Shift+V pastes.
   */
  function keyAction(event, hasSelection, mac) {
    var pass = { kind: "pass", clear: false };
    if (event.type !== "keydown" || event.altKey) return pass;
    var key = String(event.key || "").toLowerCase();
    if (mac) {
      if (key === "c" && event.metaKey && !event.ctrlKey && !event.shiftKey && hasSelection) return { kind: "native", clear: false };
      return pass;
    }
    if (!event.ctrlKey || event.metaKey) return pass;
    if (key === "v" && event.shiftKey) return { kind: "native", clear: false };
    if (key !== "c") return pass;
    if (event.shiftKey) return hasSelection ? { kind: "copy", clear: false } : { kind: "swallow", clear: false };
    return hasSelection ? { kind: "native", clear: true } : pass;
  }

  /** Drag pixels gathered into whole lines of `unit` pixels: what is left and the lines (> 0 back in time). */
  function gather(pending, px, unit) {
    var total = pending + px;
    var lines = unit > 0 ? Math.trunc(total / unit) : 0;
    return { pending: total - lines * unit, lines: lines };
  }

  // Lines and selection, the phones' rules (TerminalView.kt): a line is { runs, wrapped }, a run
  // { col, cells, flags, text } (a cluster run is one cell's text, a plain run one unit per cell).

  /** The text of cells c0..c1 of a line: blanks between runs are spaces. */
  function cellsText(line, c0, c1) {
    var out = "", at = c0;
    for (var i = 0; i < line.runs.length; i++) {
      var run = line.runs[i];
      if (run.col + run.cells <= c0 || run.col > c1) continue;
      while (at < run.col) { out += " "; at++; }
      if (run.flags & FLAG.CLUSTER) out += run.text;
      else out += run.text.slice(Math.max(0, at - run.col), Math.min(run.text.length, c1 - run.col + 1));
      at = Math.min(run.col + run.cells, c1 + 1);
    }
    return out;
  }

  /** The text from (line, col) to (line, col), inclusive: rows joined by newlines, except where a row wraps onto the next. */
  function selectionText(lineAt, from, to, cols) {
    var out = "";
    for (var i = from[0]; i <= to[0]; i++) {
      var line = lineAt(i);
      var part = cellsText(line, i === from[0] ? from[1] : 0, i === to[0] ? to[1] : cols - 1);
      if (line.wrapped && i !== to[0]) out += part;
      else {
        out += part.replace(/ +$/, "");
        if (i !== to[0]) out += "\n";
      }
    }
    // Empty rows at the end (Select All over a screen not yet full) are not text.
    return out.replace(/\n+$/, "");
  }

  /** The word under (line, col): the run of non-blank cells around it; null on a blank. */
  function wordAt(line, col, cols) {
    if (col < 0 || col >= cols) return null;
    var blank = [];
    for (var c = 0; c < cols; c++) blank.push(true);
    for (var i = 0; i < line.runs.length; i++) {
      var run = line.runs[i];
      for (var j = 0; j < run.cells && run.col + j < cols; j++)
        blank[run.col + j] = !(run.flags & FLAG.CLUSTER) && run.text[j] === " ";
    }
    if (blank[col]) return null;
    var a = col, b = col;
    while (a > 0 && !blank[a - 1]) a--;
    while (b < cols - 1 && !blank[b + 1]) b++;
    return [a, b];
  }

  /** The rows of the line row `i` is part of: up while the row above wraps, down while this one does. */
  function lineSpan(lineAt, i, count) {
    var top = i, bottom = i;
    while (top > 0 && lineAt(top - 1).wrapped) top--;
    while (bottom < count - 1 && lineAt(bottom).wrapped) bottom++;
    return [top, bottom];
  }

  /** The UTF-16 index in a line's text (cellsText from col 0) of the character at `col`; -1 past its end. */
  function textIndex(line, col) {
    var full = cellsText(line, 0, 1 << 15).replace(/ +$/, "");
    var text = cellsText(line, 0, col), end = 0;
    for (var i = 0; i < line.runs.length; i++) {
      var run = line.runs[i];
      if (run.col > col) break;
      if ((run.flags & FLAG.CLUSTER) && col < run.col + run.cells) {
        var start = text.length - run.text.length;
        return start < full.length ? start : -1;
      }
      end = Math.min(run.col + run.cells, col + 1);
    }
    // A blank cell after the runs before it counts as a space.
    var index = text.length + (col + 1 - Math.max(end, 0)) - 1;
    if (end > col) index = text.length - 1;
    return index >= 0 && index < full.length ? index : -1;
  }

  /** Where the history viewport stands: `top` the history row at the top, the history's length at the bottom. */
  function viewportOf(scrollPx, cellH, historyRows) {
    var atBottom = scrollPx <= 0;
    var top = atBottom ? historyRows : Math.min(historyRows, Math.max(0, historyRows - Math.ceil(scrollPx / cellH)));
    return { top: top, atBottom: atBottom };
  }

  /** The 256 colors: the theme's 16, then the 6×6×6 cube and 24 grays, as 0xRRGGBB. */
  function palette256(sixteen) {
    var out = sixteen.slice(0, 16);
    var steps = [0, 95, 135, 175, 215, 255];
    for (var r = 0; r < 6; r++) for (var g = 0; g < 6; g++) for (var b = 0; b < 6; b++) out.push((steps[r] << 16) | (steps[g] << 8) | steps[b]);
    for (var i = 0; i < 24; i++) { var v = 8 + i * 10; out.push((v << 16) | (v << 8) | v); }
    return out;
  }

  /** `#rrggbb` (or `#rrggbbaa`) as 0xRRGGBB. */
  function rgbOf(color) {
    var m = /^#([0-9a-f]{6})/i.exec(String(color || ""));
    return m ? parseInt(m[1], 16) : 0;
  }

  var helpers = { keyAction: keyAction, keyCode: keyCode, KEY_NAMES: KEY_NAMES, unshiftedOf: unshiftedOf, modsOf: modsOf, keyText: keyText,
    gather: gather, cellsText: cellsText, selectionText: selectionText, wordAt: wordAt, lineSpan: lineSpan, textIndex: textIndex,
    viewportOf: viewportOf, palette256: palette256, rgbOf: rgbOf };
  if (typeof module === "object" && module && module.exports) {
    module.exports = helpers;
    return;
  }

  // The page.

  var app = window.parent;
  var bridge = window.nsh && typeof window.nsh.postMessage === "function" ? window.nsh : null;
  var port = null;
  var queue = [];
  function send(message) {
    var data = JSON.stringify(message);
    if (bridge) bridge.postMessage(data);
    else if (port) port.postMessage(data);
    else queue.push(data);
  }

  var script = document.currentScript;
  var dir = script && script.src ? script.src.replace(/[^/]*$/, "") : "/terminal/";
  var box = document.getElementById("box");
  var canvas = document.getElementById("vt");
  var ctx = canvas.getContext("2d", { alpha: false });
  var input = document.getElementById("input");
  var mac = /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent || "");
  var coarse = window.matchMedia ? window.matchMedia("(any-pointer: coarse)") : null;
  var enc = new TextEncoder();
  var dec = new TextDecoder();

  // The VT (wasm): x the module's exports.
  var x = null, memory = null, loading = null, failed = false;
  var vt = 0, scratch = 0, scratchCols = 0, opsBuf = 0, outBuf = 0, frameBuf = 0;

  // What the app set.
  var theme = { background: "#000000", foreground: "#ffffff", cursor: "#ffffff", selection: "rgba(128,128,128,0.4)", palette: [] };
  var font = { size: 13, family: "monospace" };
  var vtPalette = null;

  // The grid, in device pixels.
  var dpr = 1, fontPx = 13, cellW = 8, cellH = 16, baseline = 12, thick = 1, padX = 0, padY = 0;
  var devW = 0, devH = 0, cols = 0, rows = 0;
  var widths = {};
  var glyphs = {};

  // The content: history lines above the screen's.
  var grid = [], history = [], historyAnsi = [], historyCols = 0, alternate = false, markRow = -1;
  var cursor = { col: 0, row: 0, visible: false, style: 1, blinking: false };
  var scrollPx = 0, pending = 0, lastViewport = "";
  var sel = null, focused = false, blinkOn = true, blinkTimer = 0;
  var composing = false, preedit = "";

  function fail(error) {
    failed = true;
    send({ type: "failed", message: String((error && error.message) || error || "The terminal could not start.") });
  }

  function load() {
    if (loading) return loading;
    var wasi = {
      fd_write: function (fd, iovs, n, written) {
        var v = new DataView(memory.buffer), total = 0;
        for (var i = 0; i < n; i++) total += v.getUint32(iovs + i * 8 + 4, true);
        v.setUint32(written, total, true);
        return 0;
      },
      fd_seek: function () { return 70; },
      fd_close: function () { return 0; },
    };
    loading = fetch(dir + "mewla-vt.wasm")
      .then(function (r) {
        if (!r.ok) throw new Error("The terminal did not load (terminal/mewla-vt.wasm: " + r.status + ").");
        return r.arrayBuffer();
      })
      .then(function (bytes) { return WebAssembly.instantiate(bytes, { wasi_snapshot_preview1: wasi }); })
      .then(function (result) {
        x = result.instance.exports;
        memory = x.memory;
        x._initialize();
        opsBuf = x.malloc(48 * 64);
        outBuf = x.malloc(8);
        frameBuf = x.malloc(16);
      });
    return loading;
  }

  function withBytes(bytes, fn) {
    var p = x.malloc(bytes.length || 1);
    new Uint8Array(memory.buffer, p, bytes.length).set(bytes);
    try { return fn(p, bytes.length); } finally { x.free(p); }
  }

  /** The bytes a packer call left in outBuf (pointer, length), as text. */
  function outText() {
    var v = new DataView(memory.buffer);
    return dec.decode(new Uint8Array(memory.buffer, v.getUint32(outBuf, true), v.getUint32(outBuf + 4, true)).slice());
  }

  function create() {
    failed = false;
    if (vt) x.mewla_vt_destroy(vt);
    vt = x.mewla_vt_create(Math.max(cols, 2), Math.max(rows, 1));
    if (!vt) throw new Error("The terminal could not start (libghostty-vt refused it).");
    themeVt(vt);
    grid = [];
    if (cols && rows) x.mewla_vt_resize(vt, cols, rows, cellW, cellH);
    pull(true);
  }

  function themeVt(handle) {
    if (!vtPalette) return;
    var p = x.malloc(256 * 4);
    new Uint32Array(memory.buffer, p, 256).set(vtPalette);
    x.mewla_vt_set_theme(handle, rgbOf(theme.foreground), rgbOf(theme.background), rgbOf(theme.cursor), p, 256);
    x.free(p);
  }

  // Frames: the packer's runs (TerminalVt.h) as lines.

  function decode(handle, full, each) {
    x.mewla_vt_frame(handle, full ? 1 : 0, frameBuf);
    var v = new DataView(memory.buffer);
    var meta = new Int32Array(memory.buffer, v.getUint32(frameBuf, true), v.getUint32(frameBuf + 4, true));
    var text = new Uint16Array(memory.buffer, v.getUint32(frameBuf + 8, true), v.getUint32(frameBuf + 12, true));
    var p = HEADER;
    for (var d = 0; d < meta[6]; d++) {
      var row = meta[p], n = meta[p + 1], wrapped = meta[p + 2] === 1;
      p += ROW;
      var runs = [];
      for (var i = 0; i < n; i++, p += RUN) {
        var s = meta[p + 6], len = meta[p + 7];
        runs.push({ col: meta[p], cells: meta[p + 1], fg: meta[p + 2] >>> 0, bg: meta[p + 3] >>> 0, flags: meta[p + 4], ul: meta[p + 5] >>> 0,
          text: String.fromCharCode.apply(null, text.subarray(s, s + len)) });
      }
      each(row, { runs: runs, wrapped: wrapped });
    }
    return meta;
  }

  function pull(full) {
    if (!vt) return;
    var meta = decode(vt, full, function (row, line) { if (row < grid.length) grid[row] = line; });
    if (grid.length !== meta[0]) {
      var next = [];
      for (var i = 0; i < meta[0]; i++) next.push(grid[i] || { runs: [], wrapped: false });
      grid = next;
      if (!full) return pull(true);
    }
    var blinking = meta[8] === 1;
    cursor = { col: meta[2], row: meta[3], visible: meta[4] === 1, style: meta[7], blinking: blinking };
    restartBlink();
    schedule();
  }

  /** History rows (whole lines, ANSI) drawn through the second VT a page at a time, so their colors are the VT's. */
  function renderHistory(ansiRows) {
    if (!ansiRows.length) return [];
    var width = Math.min(Math.max(historyCols || cols || 80, 2), 1000);
    if (!scratch || scratchCols !== width) {
      if (scratch) x.mewla_vt_destroy(scratch);
      scratch = x.mewla_vt_create(width, PAGE);
      scratchCols = width;
      if (scratch) themeVt(scratch);
    }
    var out = [];
    if (!scratch) {
      for (var e = 0; e < ansiRows.length; e++) out.push({ runs: [], wrapped: false });
      return out;
    }
    for (var at = 0; at < ansiRows.length; at += PAGE) {
      var page = ansiRows.slice(at, at + PAGE);
      // Clear and home without resetting the styling the page before left.
      withBytes(enc.encode("\x1b[2J\x1b[H" + page.join("\r\n")), function (p, n) { x.mewla_vt_write(scratch, p, n); });
      var rendered = [];
      decode(scratch, true, function (row, line) { if (row < PAGE) rendered[row] = line; });
      // A history row is a whole line (the computer joined wrapped rows).
      for (var i = 0; i < page.length; i++) out.push({ runs: rendered[i] ? rendered[i].runs : [], wrapped: false });
    }
    return out;
  }

  function destroyScratch() {
    if (scratch) x.mewla_vt_destroy(scratch);
    scratch = 0;
    scratchCols = 0;
  }

  // The grid: cells are whole device pixels; the face is scaled so "M" is exactly one cell wide.

  function face(flags) {
    return ((flags & FLAG.ITALIC) ? "italic " : "") + ((flags & FLAG.BOLD) ? "600 " : "400 ") + fontPx + "px " + font.family;
  }

  function measure() {
    dpr = window.devicePixelRatio || 1;
    fontPx = font.size * dpr;
    ctx.font = "400 " + fontPx + "px " + font.family;
    var m = ctx.measureText("M");
    cellW = Math.max(1, Math.round(m.width));
    fontPx = fontPx * cellW / (m.width || cellW);
    ctx.font = "400 " + fontPx + "px " + font.family;
    m = ctx.measureText("M");
    var ascent = m.fontBoundingBoxAscent || fontPx * 0.8, descent = m.fontBoundingBoxDescent || fontPx * 0.2;
    var textH = ascent + descent;
    cellH = Math.max(1, Math.round(textH));
    baseline = Math.round((cellH - textH) / 2 + ascent);
    thick = x ? x.mewla_glyph_thickness(cellH) : Math.max(1, Math.round(cellH / 14));
    padX = Math.round(PAD_X * dpr);
    padY = Math.round(PAD_Y * dpr);
    widths = {};
    glyphs = {};
  }

  /** The canvas fills the box in device pixels; the grid is what fits inside the inset. */
  function fit(w, h) {
    if (w !== undefined) { devW = w; devH = h; }
    if (!devW || !devH) return;
    measure();
    if (canvas.width !== devW || canvas.height !== devH) {
      canvas.width = devW;
      canvas.height = devH;
    }
    canvas.style.width = devW / dpr + "px";
    canvas.style.height = devH / dpr + "px";
    var c = Math.max(2, Math.floor((devW - padX) / cellW));
    var r = Math.max(1, Math.floor((devH - padY) / cellH));
    if (!x || !vt) return;
    if (c !== cols || r !== rows) {
      cols = c;
      rows = r;
      x.mewla_vt_resize(vt, cols, rows, cellW, cellH);
      send({ type: "size", cols: cols, rows: rows });
    }
    clampScroll();
    pull(true);
    placeInput();
  }

  function refit() {
    var rect = box.getBoundingClientRect();
    fit(Math.round(rect.width * (window.devicePixelRatio || 1)), Math.round(rect.height * (window.devicePixelRatio || 1)));
  }

  // Content lines: history then the screen.

  function lineCount() { return history.length + grid.length; }
  function lineAt(i) { return (i < history.length ? history[i] : grid[i - history.length]) || { runs: [], wrapped: false }; }
  /** The y of content line i, in device pixels. */
  function lineY(i) { return padY + (i - history.length) * cellH + Math.round(scrollPx); }
  function lineUnder(y) { return history.length + Math.floor((y - padY - Math.round(scrollPx)) / cellH); }
  function colUnder(px) { return Math.min(Math.max(Math.floor((px - padX) / cellW), 0), Math.max(cols - 1, 0)); }

  // Drawing.

  var queued = false;
  function schedule() {
    if (queued) return;
    queued = true;
    requestAnimationFrame(function () { queued = false; draw(); });
  }

  function css(rgb) { return "#" + (rgb & 0xffffff).toString(16).padStart(6, "0"); }
  function argb(v) {
    var a = (v >>> 24) & 255;
    return a === 255 ? css(v) : "rgba(" + ((v >> 16) & 255) + "," + ((v >> 8) & 255) + "," + (v & 255) + "," + a / 255 + ")";
  }
  function isDrawn(cp) { return (cp >= 0x2500 && cp <= 0x259f) || (cp >= 0x2800 && cp <= 0x28ff) || (cp >= 0x23f4 && cp <= 0x23fa) || (cp >= 0xe0b0 && cp <= 0xe0bf); }

  function glyphOps(cp) {
    var ops = glyphs[cp];
    if (ops) return ops;
    var n = x.mewla_glyph_ops(cp, cellW, cellH, opsBuf, 64), v = new DataView(memory.buffer);
    ops = [];
    for (var i = 0; i < n && i < 64; i++) {
      var b = opsBuf + i * 48, p = [];
      for (var k = 0; k < 8; k++) p.push(v.getFloat32(b + 16 + k * 4, true));
      ops.push({ kind: v.getInt32(b, true), points: v.getInt32(b + 4, true), alpha: v.getFloat32(b + 8, true), stroke: v.getFloat32(b + 12, true), p: p });
    }
    glyphs[cp] = ops;
    return ops;
  }

  /** A box-drawing, block, braille or Powerline glyph from TerminalGlyphs, exactly in its cell. */
  function drawGlyph(cp, x0, y, color) {
    var ops = glyphOps(cp);
    ctx.save();
    ctx.fillStyle = ctx.strokeStyle = color;
    ctx.lineCap = "butt";
    for (var i = 0; i < ops.length; i++) {
      var o = ops[i], p = o.p;
      ctx.globalAlpha = o.alpha < 1 ? o.alpha : 1;
      if (o.kind === 1) ctx.fillRect(x0 + p[0], y + p[1], p[2] - p[0], p[3] - p[1]);
      else if (o.kind === 2) {
        ctx.beginPath();
        ctx.moveTo(x0 + p[0], y + p[1]);
        for (var j = 1; j < o.points; j++) ctx.lineTo(x0 + p[2 * j], y + p[2 * j + 1]);
        ctx.closePath();
        ctx.fill();
      } else if (o.kind === 3) {
        ctx.lineWidth = o.stroke;
        ctx.beginPath();
        ctx.moveTo(x0 + p[0], y + p[1]);
        ctx.lineTo(x0 + p[2], y + p[3]);
        ctx.stroke();
      } else if (o.kind === 4 || o.kind === 5) {
        ctx.beginPath();
        ctx.moveTo(x0 + p[0], y + p[1]);
        ctx.bezierCurveTo(x0 + p[2], y + p[3], x0 + p[4], y + p[5], x0 + p[6], y + p[7]);
        if (o.kind === 5) { ctx.closePath(); ctx.fill(); } else { ctx.lineWidth = o.stroke; ctx.stroke(); }
      } else if (o.kind === 6) {
        ctx.beginPath();
        ctx.ellipse(x0 + (p[0] + p[2]) / 2, y + (p[1] + p[3]) / 2, (p[2] - p[0]) / 2, (p[3] - p[1]) / 2, 0, 0, 2 * Math.PI);
        ctx.fill();
      }
    }
    ctx.restore();
  }

  /** Whether `c` in the current face is exactly one cell wide (then runs of such characters draw as one string). */
  function fits(c) {
    var key = ctx.font + "\u0000" + c;
    var w = widths[key];
    if (w === undefined) widths[key] = w = ctx.measureText(c).width;
    return Math.abs(w - cellW) < 0.05;
  }

  /** A glyph that is not one cell wide (CJK, emoji, a symbol from a fallback face): centred in its `cells`, scaled down to fit. */
  function drawFitted(text, x0, y, cells) {
    var room = cells * cellW, w = ctx.measureText(text).width;
    if (w <= room + 0.5 || w <= 0) {
      ctx.fillText(text, x0 + (room - w) / 2, y + baseline);
      return;
    }
    var s = room / w, cx = x0 + room / 2, cy = y + cellH / 2;
    ctx.save();
    ctx.translate(cx, cy);
    ctx.scale(s, s);
    ctx.translate(-cx, -cy);
    ctx.fillText(text, x0 + (room - w) / 2, y + baseline);
    ctx.restore();
  }

  /** Underline (its style and color), strikethrough and overline across the run's cells, one line weight. */
  function decorate(run, x0, y, color) {
    var x1 = x0 + run.cells * cellW, t = thick, style = (run.flags >> UNDERLINE_SHIFT) & 7;
    if (style) {
      ctx.fillStyle = ctx.strokeStyle = run.ul ? argb(run.ul) : color;
      // Below the baseline, kept inside the cell with room for a double or curly line.
      var under = Math.min(y + baseline + Math.max(t, Math.round(fontPx * 0.12)), y + cellH - 3 * t);
      if (style === 2) {
        ctx.fillRect(x0, under, x1 - x0, t);
        ctx.fillRect(x0, under + 2 * t, x1 - x0, t);
      } else if (style === 3) {
        ctx.beginPath();
        ctx.moveTo(x0, under + t);
        for (var cx = x0, up = true, half = cellW / 2; cx < x1; cx += half, up = !up)
          ctx.quadraticCurveTo(cx + half / 2, up ? under - t : under + 3 * t, Math.min(cx + half, x1), under + t);
        ctx.lineWidth = t;
        ctx.stroke();
      } else if (style === 4) {
        for (var dx = x0; dx < x1; dx += 2 * t) ctx.fillRect(dx, under, Math.min(t, x1 - dx), t);
      } else if (style === 5) {
        var dash = Math.max(2 * t, Math.round(cellW * 0.4));
        for (var ex = x0; ex < x1; ex += 2 * dash) ctx.fillRect(ex, under, Math.min(dash, x1 - ex), t);
      } else ctx.fillRect(x0, under, x1 - x0, t);
    }
    ctx.fillStyle = color;
    if (run.flags & FLAG.STRIKE) ctx.fillRect(x0, y + Math.round(cellH / 2) - Math.floor(t / 2), x1 - x0, t);
    if (run.flags & FLAG.OVERLINE) ctx.fillRect(x0, y, x1 - x0, t);
  }

  /** `next` is the run after this one in its row (a flag's second regional indicator). */
  function drawRun(run, y, color, next) {
    var x0 = padX + run.col * cellW;
    ctx.font = face(run.flags);
    ctx.fillStyle = color;
    if (run.flags & FLAG.FLAG_TAIL) {
      // A flag's second regional indicator: the first draws the flag across both.
      decorate(run, x0, y, color);
      return;
    }
    if (!(run.flags & FLAG.CLUSTER)) {
      var seg = -1;
      for (var i = 0; i <= run.text.length; i++) {
        var c = i < run.text.length ? run.text[i] : " ";
        var code = c.charCodeAt(0);
        var drawn = i < run.text.length && isDrawn(code);
        var one = i < run.text.length && c !== " " && !drawn && fits(c);
        if (seg >= 0 && !one) {
          ctx.fillText(run.text.slice(seg, i), x0 + seg * cellW, y + baseline);
          seg = -1;
        }
        if (i === run.text.length || c === " ") continue;
        if (drawn) { drawGlyph(code, x0 + i * cellW, y, color); ctx.fillStyle = color; }
        else if (!one) drawFitted(c, x0 + i * cellW, y, 1);
        else if (seg < 0) seg = i;
      }
    } else if (run.text.length) {
      var cp = run.text.codePointAt(0);
      if (String.fromCodePoint(cp).length === run.text.length && isDrawn(cp)) drawGlyph(cp, x0, y, color);
      // Two regional indicators: the one flag, centred across both runs' cells.
      else if ((run.flags & FLAG.FLAG_HEAD) && next) drawFitted(run.text + next.text, x0, y, run.cells + next.cells);
      // An emoji the VT gives one cell, with a blank cell after it: full size across both.
      else if (run.flags & FLAG.EMOJI_WIDE) drawFitted(run.text, x0, y, 2);
      else drawFitted(run.text, x0, y, run.cells);
    }
    ctx.fillStyle = color;
    decorate(run, x0, y, color);
  }

  function ordered() {
    if (!sel) return null;
    var a = sel.a, b = sel.b;
    return a[0] < b[0] || (a[0] === b[0] && a[1] <= b[1]) ? [a, b] : [b, a];
  }

  function draw() {
    ctx.fillStyle = theme.background;
    ctx.fillRect(0, 0, canvas.width, canvas.height);
    if (!x || cellH <= 0) return;
    var first = Math.max(0, lineUnder(0)), last = Math.min(lineCount() - 1, lineUnder(canvas.height - 1));
    var s = ordered();
    for (var i = first; i <= last; i++) {
      var y = lineY(i), line = lineAt(i), j;
      for (j = 0; j < line.runs.length; j++) {
        var run = line.runs[j];
        if (run.bg) { ctx.fillStyle = argb(run.bg); ctx.fillRect(padX + run.col * cellW, y, run.cells * cellW, cellH); }
      }
      if (i < history.length && i === markRow) {
        ctx.fillStyle = theme.selection;
        ctx.fillRect(0, y, canvas.width, cellH);
      }
      if (s && i >= s[0][0] && i <= s[1][0]) {
        var c0 = i === s[0][0] ? s[0][1] : 0, c1 = i === s[1][0] ? s[1][1] : cols - 1;
        if (c1 >= c0) { ctx.fillStyle = theme.selection; ctx.fillRect(padX + c0 * cellW, y, (c1 + 1 - c0) * cellW, cellH); }
      }
      for (j = 0; j < line.runs.length; j++) drawRun(line.runs[j], y, argb(line.runs[j].fg), line.runs[j + 1]);
    }
    drawCursor();
  }

  function runAt(row, col) {
    var line = grid[row];
    if (!line) return null;
    for (var i = 0; i < line.runs.length; i++) {
      var r = line.runs[i];
      if (col >= r.col && col < r.col + r.cells) return r;
    }
    return null;
  }

  function drawCursor() {
    var line = history.length + cursor.row;
    var x0 = padX + cursor.col * cellW, y = lineY(line);
    if (y + cellH <= 0 || y >= canvas.height) return;
    if (composing && preedit) {
      // The composition, at the cursor, over the cells it covers, underlined.
      ctx.font = face(0);
      var w = Math.max(cellW, Math.ceil(ctx.measureText(preedit).width / cellW) * cellW);
      ctx.fillStyle = theme.background;
      ctx.fillRect(x0, y, w, cellH);
      ctx.fillStyle = theme.foreground;
      ctx.fillText(preedit, x0, y + baseline);
      ctx.fillRect(x0, y + cellH - thick, w, thick);
      return;
    }
    if (!cursor.visible || (focused && cursor.blinking && !blinkOn)) return;
    var color = theme.cursor;
    if (!focused) {
      ctx.strokeStyle = color;
      ctx.lineWidth = thick;
      ctx.strokeRect(x0 + thick / 2, y + thick / 2, cellW - thick, cellH - thick);
      return;
    }
    if (cursor.style === 0) { ctx.fillStyle = color; ctx.fillRect(x0, y, Math.max(thick, Math.round(cellW / 8)), cellH); return; }
    if (cursor.style === 2) { ctx.fillStyle = color; ctx.fillRect(x0, y + cellH - 2 * thick, cellW, 2 * thick); return; }
    if (cursor.style === 3) {
      ctx.strokeStyle = color;
      ctx.lineWidth = thick;
      ctx.strokeRect(x0 + thick / 2, y + thick / 2, cellW - thick, cellH - thick);
      return;
    }
    var under = runAt(cursor.row, cursor.col);
    var wide = under && (under.flags & FLAG.CLUSTER) && under.cells === 2;
    ctx.fillStyle = color;
    ctx.fillRect(wide ? padX + under.col * cellW : x0, y, (wide ? 2 : 1) * cellW, cellH);
    if (under) {
      // The character under a block, in the background's color.
      var single = under.flags & FLAG.CLUSTER ? under
        : { col: cursor.col, cells: 1, fg: 0, bg: 0, flags: under.flags & ~(7 << UNDERLINE_SHIFT), ul: 0, text: under.text[cursor.col - under.col] || " " };
      drawRun(single, y, theme.background, null);
    }
  }

  function restartBlink() {
    clearInterval(blinkTimer);
    blinkOn = true;
    if (focused && cursor.blinking) blinkTimer = setInterval(function () { blinkOn = !blinkOn; schedule(); }, 600);
  }

  /** The textarea sits on the cursor's cell, so the input method's window opens there. */
  function placeInput() {
    var y = lineY(history.length + cursor.row);
    input.style.left = (padX + cursor.col * cellW) / dpr + "px";
    input.style.top = Math.max(0, y) / dpr + "px";
    input.style.height = cellH / dpr + "px";
    input.style.lineHeight = cellH / dpr + "px";
    input.style.fontSize = font.size + "px";
  }

  // History and scrolling (device pixels from the bottom: 0 shows the screen).

  function maxScroll() { return alternate ? 0 : history.length * cellH; }
  function clampScroll() { scrollPx = Math.min(Math.max(scrollPx, 0), maxScroll()); }
  function report() {
    var v = viewportOf(scrollPx, cellH, history.length);
    var key = v.top + "," + v.atBottom;
    if (key === lastViewport) return;
    lastViewport = key;
    send({ type: "viewport", top: v.top, atBottom: v.atBottom });
  }
  function setScroll(px) {
    scrollPx = px;
    clampScroll();
    report();
    placeInput();
    schedule();
  }

  function applyHistory(m) {
    if (m.reset) {
      history = [];
      historyAnsi = [];
      markRow = -1;
      sel = null;
      destroyScratch();
    } else if (m.removed > 0) {
      var n = Math.min(m.removed, history.length);
      history.splice(0, n);
      historyAnsi.splice(0, n);
      markRow = markRow >= n ? markRow - n : -1;
      shiftSelection(n);
    }
    if (m.cols > 0 && m.cols !== historyCols) {
      historyCols = m.cols;
      destroyScratch();
    }
    alternate = m.alternate;
    for (var i = 0; i < m.rows.length; i++) historyAnsi.push(m.rows[i]);
    var added = renderHistory(m.rows);
    for (var j = 0; j < added.length; j++) history.push(added[j]);
    // Scrolled up, the rows above the screen grew: the reader's rows stay where they are.
    if (scrollPx > 0 && !m.reset) scrollPx += m.rows.length * cellH;
    if (alternate) scrollPx = 0;
    clampScroll();
    report();
    placeInput();
    schedule();
  }

  /** History rows removed from the front pull every content line up by `removed`. */
  function shiftSelection(removed) {
    if (!sel) return;
    sel.a = [sel.a[0] - removed, sel.a[1]];
    sel.b = [sel.b[0] - removed, sel.b[1]];
    var s = ordered();
    if (s[1][0] < 0) sel = null;
    else if (s[0][0] < 0) { s[0][0] = 0; s[0][1] = 0; sel = { a: s[0], b: s[1] }; }
  }

  /** A drag of `px` CSS pixels (> 0 back in time): the history scrolls, else the program gets lines of `unit` CSS pixels. */
  function drag(px, unit) {
    if (alternate || history.length === 0) {
      var step = gather(pending, px, unit);
      pending = step.pending;
      if (step.lines !== 0) send({ type: "scroll", lines: step.lines });
      return;
    }
    pending = 0;
    setScroll(scrollPx + px * dpr);
  }

  box.addEventListener("wheel", function (e) {
    e.preventDefault();
    e.stopPropagation();
    var px = e.deltaMode === 1 ? e.deltaY * WHEEL_LINE_PX : e.deltaMode === 2 ? e.deltaY * box.clientHeight : e.deltaY;
    drag(-px, WHEEL_LINE_PX);
  }, { capture: true, passive: false });

  var touchY = null;
  box.addEventListener("touchstart", function (e) {
    touchY = e.touches.length === 1 ? e.touches[0].clientY : null;
  }, { capture: true, passive: true });
  box.addEventListener("touchmove", function (e) {
    if (touchY === null || e.touches.length !== 1) return;
    var y = e.touches[0].clientY;
    var dy = y - touchY;
    touchY = y;
    e.preventDefault();
    drag(dy, cellH / dpr || 16);
  }, { capture: true, passive: false });
  box.addEventListener("touchend", function () { touchY = null; }, { capture: true, passive: true });

  // The pointer: selection (drag, double-click a word, triple-click a line), a program's mouse reports,
  // taps (a link opens: the app decides what is one).

  function pointAt(e) {
    var rect = canvas.getBoundingClientRect();
    return { x: (e.clientX - rect.left) * dpr, y: (e.clientY - rect.top) * dpr };
  }

  /** Whether the program gets this pointer event: it asked for reports and neither Shift nor (on a Mac) Option forces a selection. */
  function reportsMouse(e) {
    return !!vt && x.mewla_vt_mouse_tracking(vt) && !e.shiftKey && !(mac && e.altKey) && scrollPx === 0;
  }

  function mouseReport(e, action) {
    var p = pointAt(e);
    var button = action === MOUSE.MOTION && !(e.buttons & 7) ? 0 : e.button === 1 ? 3 : e.button === 2 ? 2 : 1;
    var mods = (e.shiftKey ? MODS.SHIFT : 0) | (e.ctrlKey ? MODS.CTRL : 0) | (e.altKey ? MODS.ALT : 0) | (e.metaKey ? MODS.SUPER : 0);
    if (x.mewla_vt_mouse(vt, action, button, mods, p.x - padX, p.y - padY, outBuf, outBuf + 4)) send({ type: "input", data: outText() });
  }

  var down = null, unit = 1, anchor = null, reporting = false, pressed = false;

  /** The keyboard's place: the textarea, once a press on the canvas ends (Firefox leaves the frame's body focused). */
  function takeFocus() {
    if (document.activeElement !== input) input.focus({ preventScroll: true });
  }

  canvas.addEventListener("mousedown", function (e) {
    if (e.button !== 0 && !reportsMouse(e)) return;
    e.preventDefault();
    pressed = true;
    takeFocus();
    if (reportsMouse(e)) {
      reporting = true;
      mouseReport(e, MOUSE.PRESS);
      return;
    }
    var p = pointAt(e);
    var at = [Math.min(Math.max(lineUnder(p.y), 0), Math.max(lineCount() - 1, 0)), colUnder(p.x)];
    down = { x: e.clientX, y: e.clientY, at: at };
    unit = Math.min(e.detail || 1, 3);
    if (unit === 1) {
      sel = null;
      anchor = [at, at];
    } else if (unit === 2) {
      var w = wordAt(lineAt(at[0]), at[1], cols);
      anchor = w ? [[at[0], w[0]], [at[0], w[1]]] : [at, at];
      sel = w ? { a: anchor[0], b: anchor[1] } : null;
    } else {
      var l = lineSpan(lineAt, at[0], lineCount());
      anchor = [[l[0], 0], [l[1], cols - 1]];
      sel = { a: anchor[0], b: anchor[1] };
    }
    schedule();
  });

  window.addEventListener("mousemove", function (e) {
    if (reporting) { mouseReport(e, MOUSE.MOTION); return; }
    if (!down && vt && x && x.mewla_vt_mouse_tracking(vt) && e.target === canvas && !e.shiftKey) { mouseReport(e, MOUSE.MOTION); return; }
    if (!down) return;
    var p = pointAt(e);
    var at = [Math.min(Math.max(lineUnder(p.y), 0), Math.max(lineCount() - 1, 0)), colUnder(p.x)];
    if (unit === 1) {
      if (Math.abs(e.clientX - down.x) <= TAP_SLOP_PX && Math.abs(e.clientY - down.y) <= TAP_SLOP_PX) return;
      sel = { a: anchor[0], b: at };
    } else {
      var span;
      if (unit === 2) {
        var w = wordAt(lineAt(at[0]), at[1], cols) || [at[1], at[1]];
        span = [[at[0], w[0]], [at[0], w[1]]];
      } else {
        var l = lineSpan(lineAt, at[0], lineCount());
        span = [[l[0], 0], [l[1], cols - 1]];
      }
      var before = span[0][0] < anchor[0][0] || (span[0][0] === anchor[0][0] && span[0][1] < anchor[0][1]);
      sel = before ? { a: span[0], b: anchor[1] } : { a: anchor[0], b: span[1] };
    }
    // Dragging past the top or bottom scrolls the history.
    if (p.y < 0) setScroll(scrollPx + cellH);
    else if (p.y > canvas.height) setScroll(scrollPx - cellH);
    schedule();
  });

  window.addEventListener("mouseup", function (e) {
    if (pressed) {
      pressed = false;
      takeFocus();
    }
    if (reporting) {
      reporting = false;
      mouseReport(e, MOUSE.RELEASE);
      return;
    }
    var start = down;
    down = null;
    if (!start || unit !== 1 || sel) return;
    if (Math.abs(e.clientX - start.x) > TAP_SLOP_PX || Math.abs(e.clientY - start.y) > TAP_SLOP_PX) return;
    tap(start.at[0], start.at[1]);
  });

  canvas.addEventListener("contextmenu", function (e) { if (reportsMouse(e)) e.preventDefault(); });

  /**
   * A tap on a cell: the app opens a link there, or focuses. A hyperlink (OSC 8) is handed over as
   * the whole text with no cell (col and row -1). History rows have no screen row (-1).
   */
  function tap(i, col) {
    if (i >= history.length && vt && x.mewla_vt_link(vt, col, i - history.length, outBuf, outBuf + 4)) {
      send({ type: "tapCell", col: -1, row: -1, text: outText(), index: 0 });
      return;
    }
    var line = lineAt(i);
    var text = cellsText(line, 0, 1 << 15).replace(/ +$/, "");
    var row = i < history.length ? -1 : i - history.length;
    send({ type: "tapCell", col: col, row: row, text: text, index: textIndex(line, col) });
  }

  // The keyboard and the input method.

  function hasSelection() { return sel !== null && selectedText() !== ""; }
  function selectedText() {
    var s = ordered();
    return s ? selectionText(lineAt, s[0], s[1], cols) : "";
  }
  function clearSelection() { if (sel) { sel = null; schedule(); } }

  /** A copy the user asked for: in this page if the browser lets it, else by the app (the page is another origin). */
  var copiedAt = 0;
  function copySelection() {
    var text = selectedText();
    if (!text) return;
    var copied = false;
    try {
      copied = document.execCommand("copy");
    } catch (error) {
      copied = false;
    }
    if (!copied) send({ type: "copy", text: text });
  }

  // At the document: the whole frame is the terminal, and Firefox may aim these at its body.
  document.addEventListener("copy", function (e) {
    var text = selectedText();
    if (!text || !e.clipboardData) return;
    e.clipboardData.setData("text/plain", text);
    e.preventDefault();
    copiedAt = Date.now();
  });
  document.addEventListener("cut", function (e) { e.preventDefault(); });
  document.addEventListener("paste", function (e) {
    e.preventDefault();
    var text = e.clipboardData ? e.clipboardData.getData("text/plain") : "";
    if (!text || !vt) return;
    withBytes(enc.encode(text), function (p, n) {
      if (x.mewla_vt_paste(vt, p, n, outBuf, outBuf + 4)) input_(outText());
    });
  });

  /** Bytes for the program: the screen follows (the history scrolls down, the selection goes). */
  function input_(data) {
    if (!data) return;
    if (scrollPx > 0) setScroll(0);
    clearSelection();
    send({ type: "input", data: data });
  }

  input.addEventListener("keydown", function (e) {
    if (e.isComposing || e.keyCode === 229 || composing) return;
    var action = keyAction(e, hasSelection(), mac);
    if (action.kind === "native") {
      var at = Date.now();
      // The browser copies next (a `copy` event) or pastes; if it did not copy, copy here.
      setTimeout(function () {
        if (String(e.key).toLowerCase() === "c" && copiedAt < at) copySelection();
        if (action.clear) clearSelection();
      }, 0);
      return;
    }
    if (action.kind === "copy" || action.kind === "swallow") {
      e.preventDefault();
      if (action.kind === "copy") copySelection();
      return;
    }
    if (!vt || e.key === "Dead" || e.key === "Process" || e.key === "Unidentified") return;
    // ⌘ shortcuts on a Mac are the browser's and the app's (⌘K, ⌘N...), not the program's.
    if (mac && e.metaKey) return;
    var text = keyText(e);
    var mods = modsOf(e, mac);
    var bytes = enc.encode(text);
    var sent = withBytes(bytes, function (p, n) {
      return x.mewla_vt_key(vt, keyCode(e.code), mods, e.repeat ? KEY_REPEAT : KEY_PRESS, p, n, unshiftedOf(e.code), outBuf, outBuf + 4);
    });
    if (sent) {
      e.preventDefault();
      input_(outText());
    }
  });

  // Text that comes without a key the encoder knows (a dictation, an emoji picker): as typed.
  input.addEventListener("input", function (e) {
    if (composing || e.isComposing || /Composition/.test(e.inputType || "")) return;
    if (e.inputType === "insertText" && e.data) input_(e.data);
    else if (e.inputType === "insertLineBreak") input_("\r");
    if (!composing) input.value = "";
  });
  input.addEventListener("compositionstart", function () {
    composing = true;
    preedit = "";
    placeInput();
    schedule();
  });
  input.addEventListener("compositionupdate", function (e) {
    preedit = e.data || "";
    schedule();
  });
  input.addEventListener("compositionend", function (e) {
    composing = false;
    preedit = "";
    // Committed once: the input events around it are the composition's, not more text.
    input_(e.data || "");
    input.value = "";
    schedule();
  });

  function focusChanged(next) {
    if (focused === next) return;
    focused = next;
    restartBlink();
    if (vt && x.mewla_vt_focus(vt, next, outBuf, outBuf + 4)) send({ type: "input", data: outText() });
    schedule();
  }
  input.addEventListener("focus", function () { focusChanged(true); });
  input.addEventListener("blur", function () { focusChanged(false); });
  // The frame focused as a whole (a click from the app's page, Tab): the keyboard goes to the textarea.
  window.addEventListener("focus", function () { if (vt) takeFocus(); });

  // Commands from the app.

  function applyTheme(next) {
    theme = {
      background: next.background,
      foreground: next.foreground,
      cursor: next.cursor,
      selection: next.selection,
      palette: next.palette || [],
    };
    document.documentElement.style.background = theme.background;
    var sixteen = [];
    for (var i = 0; i < 16; i++) sixteen.push(rgbOf(theme.palette[i] || theme.foreground));
    vtPalette = palette256(sixteen);
    if (!x) return;
    if (vt) themeVt(vt);
    // The history's colors are the VT's: draw its rows again.
    destroyScratch();
    history = renderHistory(historyAnsi);
    pull(true);
  }

  function applyFont(next) {
    font = { size: next.size, family: next.family };
    loadFaces().then(function () { refit(); schedule(); }, function () { refit(); });
    refit();
  }

  /** The bundled faces (the page's @font-face) load once something asks for them. */
  function loadFaces() {
    if (!document.fonts || !document.fonts.load) return Promise.resolve();
    var wanted = font.size + "px " + font.family;
    return Promise.all([document.fonts.load(wanted), document.fonts.load("600 " + wanted),
      document.fonts.load("italic " + wanted), document.fonts.load("italic 600 " + wanted)]);
  }

  function probe() {
    var text = [];
    for (var i = 0; i < grid.length; i++) text.push(cellsText(grid[i], 0, cols - 1).replace(/ +$/, ""));
    var joined = text.join("\n").replace(/\s+$/, "");
    var gridText = (vt ? cols + "x" + rows : "no terminal") + " cell " + cellW + "x" + cellH + " dpr " + dpr + " box " + devW + "x" + devH;
    send({ type: "probe", text: joined, drawn: joined, history: history.length, grid: gridText });
  }

  function write(data) {
    if (!vt) return;
    withBytes(enc.encode(data), function (p, n) { x.mewla_vt_write(vt, p, n); });
    // A program's copy (OSC 52 and the like): the app puts it on the clipboard.
    if (x.mewla_vt_take_clipboard(vt, outBuf, outBuf + 4)) {
      var text = outText();
      if (text && text.length <= MAX_COPY_CHARS) send({ type: "copy", text: text });
    }
    pull(false);
    placeInput();
  }

  function start() {
    return load().then(function () {
      create();
      refit();
    }).catch(fail);
  }

  /** Makes the terminal again (after `failed`): the VT loads again if it never did. Its `size` clears the failure. */
  function retry() {
    if (!x) loading = null;
    cols = 0;
    rows = 0;
    start();
  }

  var waiting = [];
  function handle(m) {
    if (!m || typeof m.type !== "string") return;
    switch (m.type) {
      case "theme": applyTheme(m.theme); break;
      case "font": applyFont(m); break;
      case "write": write(m.data); break;
      case "reset": write("\x1bc"); sel = null; break;
      // Only a running terminal takes the focus (one that failed leaves it where it is).
      case "focus": if (vt) input.focus({ preventScroll: true }); break;
      case "blur": input.blur(); break;
      case "history": applyHistory(m); break;
      case "scrollTo": if (!alternate && history.length) setScroll((history.length - Math.min(Math.max(m.row, 0), history.length)) * cellH); break;
      case "scrollToBottom": setScroll(0); break;
      case "mark": markRow = m.row; schedule(); break;
      case "retry": retry(); break;
      case "probe": probe(); break;
    }
  }

  function run(batch) {
    for (var j = 0; j < batch.length; j++) {
      try {
        handle(batch[j]);
      } catch (error) {
        send({ type: "failed", message: "The terminal page failed (" + batch[j].type + "): " + String((error && error.message) || error) });
      }
    }
  }

  addEventListener("message", function (e) {
    if (e.source !== app) return;
    // NSH's handshake: the port to the app (its bridge takes it where it runs).
    if (!bridge && e.ports && e.ports[0]) {
      port = e.ports[0];
      for (var i = 0; i < queue.length; i++) port.postMessage(queue[i]);
      queue = [];
      return;
    }
    if (typeof e.data !== "string") return;
    var batch;
    try {
      batch = JSON.parse(e.data);
    } catch (error) {
      return;
    }
    if (!Array.isArray(batch)) return;
    // Until the VT runs, theme and font apply and the rest waits, in order.
    if (!vt) {
      for (var w = 0; w < batch.length; w++) {
        if (batch[w] && (batch[w].type === "theme" || batch[w].type === "font")) run([batch[w]]);
        else waiting.push(batch[w]);
      }
      return;
    }
    run(batch);
  });

  // The box in device pixels (exact where the browser says), and the ratio when it changes (a zoom, another screen).
  try {
    new ResizeObserver(function (entries) {
      var entry = entries[0];
      var size = entry.devicePixelContentBoxSize && entry.devicePixelContentBoxSize[0];
      if (size) fit(size.inlineSize, size.blockSize);
      else refit();
    }).observe(box, { box: "device-pixel-content-box" });
  } catch (error) {
    if (window.ResizeObserver) new ResizeObserver(refit).observe(box);
    else addEventListener("resize", refit);
  }
  (function watchRatio() {
    if (!window.matchMedia) return;
    var query = window.matchMedia("(resolution: " + (window.devicePixelRatio || 1) + "dppx)");
    var once = function () { refit(); watchRatio(); };
    if (query.addEventListener) query.addEventListener("change", once, { once: true });
  })();
  // Cells measured with a fallback face are wrong once the real one arrives.
  if (document.fonts) document.fonts.addEventListener("loadingdone", function () { refit(); });
  if (coarse && coarse.addEventListener) coarse.addEventListener("change", function () { send({ type: "pointer", touch: coarse.matches }); });

  start().then(function () {
    var held = waiting;
    waiting = [];
    run(held);
  });
  send({ type: "ready", touch: coarse ? coarse.matches : false });
})();
