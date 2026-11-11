// The browser's terminal page: xterm.js (vendored beside this file, see vendor.json) as the
// VT, under the history the computer keeps. The app builds the page
// (client/core/terminal/webSurface.ts terminalPageHtml) and speaks the terminal
// view's contract for it (client/ui/terminal/web/WebTerminalSurface.tsx).
//
// It runs in NSH's web view, an iframe sandboxed to an origin of its own whose
// document inherits the app page's Content-Security-Policy: no inline script,
// no eval, classic scripts from the app's origin only. So this is a plain
// classic script, and it reaches the app over the message port NSH hands the
// page on load (NSH's own `window.nsh` bridge is an inline script, which that
// policy blocks; where it runs, it is used instead).
//
// Layout: one scroll box, the history rows (DOM, styled by the app) above the
// screen (xterm, no scrollback of its own: the history is the computer's).
// Wheel and touch drags scroll the history; with none, or with the program on
// the alternate screen, they go to the app as `scroll` lines.

(function () {
  "use strict";

  /** Wheel pixels per line (a notch is about three). */
  var WHEEL_LINE_PX = 40;
  /** How far a press may move and still be a tap. */
  var TAP_SLOP_PX = 4;
  var MAX_COPY_CHARS = 1 << 20;

  /**
   * What a key press does with the clipboard (Expo's terminalClipboard.ts):
   * `native` lets the browser copy or paste (the terminal stays out), `copy`
   * copies the selection, `swallow` keeps the key from the program, `pass`
   * is the terminal's. ⌘C copies on a Mac; elsewhere Ctrl+Shift+C always
   * belongs to the terminal and plain Ctrl+C copies only while text is
   * selected (so with nothing selected it still interrupts), dropping the
   * selection so the next one interrupts. Ctrl+Shift+V pastes.
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

  /**
   * The UTF-16 index in a row's text (`translateToString(true)`, `length`
   * long) of cell `cells.length - 1`, each cell `[chars, width]`: an empty
   * cell is one space, the right half of a wide character (width 0) is that
   * character. -1 past the text's end.
   */
  function utf16Index(cells, length) {
    var index = 0;
    var start = 0;
    var last = cells.length - 1;
    for (var i = 0; i <= last; i++) {
      var chars = cells[i][0];
      var width = cells[i][1];
      if (width === 0) {
        if (i === last) return start < length ? start : -1;
        continue;
      }
      if (i === last) return index < length ? index : -1;
      start = index;
      index += chars ? chars.length : 1;
    }
    return -1;
  }

  /** Drag pixels gathered into whole lines of `unit` pixels: what is left and the lines (> 0 back in time). */
  function gather(pending, px, unit) {
    var total = pending + px;
    var lines = unit > 0 ? Math.trunc(total / unit) : 0;
    return { pending: total - lines * unit, lines: lines };
  }

  /** Where the history viewport stands: `top` the row at the top, the history's length at the bottom. */
  function viewportOf(scrollTop, scrollMax, rowHeight, historyRows) {
    var atBottom = scrollTop >= scrollMax - 1;
    var top = atBottom ? historyRows : Math.min(historyRows, Math.max(0, Math.floor(scrollTop / rowHeight)));
    return { top: top, atBottom: atBottom };
  }

  /** A program's OSC 52 (`c;<base64>`) as text for the clipboard; null for a query, another selection or junk. */
  function osc52Text(data) {
    var semi = data.indexOf(";");
    if (semi < 0) return null;
    var target = data.slice(0, semi);
    var payload = data.slice(semi + 1);
    // tmux sends an empty selection or "c"; "p" and the cut buffers are X11's.
    if ((target !== "" && target.indexOf("c") < 0) || payload === "?" || payload.length > MAX_COPY_CHARS * 2) return null;
    try {
      var binary = atob(payload);
      var bytes = new Uint8Array(binary.length);
      for (var i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      var text = new TextDecoder().decode(bytes);
      return text && text.length <= MAX_COPY_CHARS ? text : null;
    } catch (error) {
      return null;
    }
  }

  var helpers = { keyAction: keyAction, utf16Index: utf16Index, gather: gather, viewportOf: viewportOf, osc52Text: osc52Text };
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

  var scroller = document.getElementById("scroller");
  var historyBox = document.getElementById("history");
  var screen = document.getElementById("screen");
  var root = document.documentElement.style;
  var mac = /Mac|iPhone|iPad|iPod/i.test(navigator.platform || navigator.userAgent || "");
  var coarse = window.matchMedia ? window.matchMedia("(any-pointer: coarse)") : null;
  var xtermScript = document.querySelector("script[data-xterm]");

  var term = null;
  var theme = null;
  var font = { size: 13, family: "monospace" };
  var cell = { w: 0, h: 0 };
  var size = { cols: 0, rows: 0 };
  var alternate = false;
  var marked = null;
  var pending = 0;
  var lastViewport = "";
  var linkAt = 0;
  var copiedAt = 0;

  function fail(error) {
    term = null;
    send({ type: "failed", message: String((error && error.message) || error || "The terminal could not start.") });
  }

  function create() {
    if (typeof window.Terminal !== "function") {
      fail("The terminal did not load (terminal/xterm.js).");
      return;
    }
    screen.replaceChildren();
    try {
      term = new window.Terminal({
        cursorBlink: true,
        fontFamily: font.family,
        fontSize: font.size,
        // A program that reports the mouse (tmux, vim, Claude Code) owns plain
        // drags; Shift selects instead, and on a Mac, where Shift does not, Option does.
        macOptionClickForcesSelection: true,
        scrollback: 0,
        theme: theme || undefined,
        // OSC 8 hyperlinks: the app opens them as it opens any link it is handed.
        linkHandler: {
          allowNonHttpProtocols: true,
          activate: function (event, uri) {
            linkAt = Date.now();
            send({ type: "tapCell", col: -1, row: -1, text: uri, index: 0 });
          },
        },
      });
      term.open(screen);
    } catch (error) {
      fail(error);
      return;
    }
    var mine = term;
    term.onData(function (data) { if (term === mine) send({ type: "input", data: data }); });
    term.onBinary(function (data) { if (term === mine) send({ type: "input", data: data }); });
    term.attachCustomKeyEventHandler(onKey);
    // The page scrolls (wheel, below); the terminal never does.
    term.attachCustomWheelEventHandler(function () { return false; });
    term.parser.registerOscHandler(52, function (data) {
      var text = osc52Text(data);
      if (text) send({ type: "copy", text: text });
      return true;
    });
    size = { cols: 0, rows: 0 };
    fit();
  }

  /** The grid that fits the screen box (FitAddon's measure, without its scroll bar: no scrollback). */
  function fit() {
    if (!term) return;
    var service = term._core && term._core._renderService;
    var dims = service && service.dimensions;
    var w = dims && dims.css.cell.width;
    var h = dims && dims.css.cell.height;
    if (!w || !h) return;
    if (w !== cell.w || h !== cell.h) {
      cell = { w: w, h: h };
      root.setProperty("--cell-w", w + "px");
      root.setProperty("--cell-h", h + "px");
    }
    var style = getComputedStyle(screen);
    var width = screen.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight);
    var height = screen.clientHeight - parseFloat(style.paddingTop) - parseFloat(style.paddingBottom);
    if (width <= 0 || height <= 0) return;
    var cols = Math.max(2, Math.floor(width / w));
    var rows = Math.max(1, Math.floor(height / h));
    if (cols !== term.cols || rows !== term.rows) {
      if (service.clear) service.clear();
      term.resize(cols, rows);
    }
    if (cols !== size.cols || rows !== size.rows) {
      size = { cols: cols, rows: rows };
      send({ type: "size", cols: cols, rows: rows });
    }
    if (isAtBottom()) toBottom();
  }

  function applyTheme(next) {
    theme = next;
    root.setProperty("--bg", next.background);
    root.setProperty("--fg", next.foreground);
    root.setProperty("--sel", next.selectionBackground);
    var names = ["black", "red", "green", "yellow", "blue", "magenta", "cyan", "white",
      "brightBlack", "brightRed", "brightGreen", "brightYellow", "brightBlue", "brightMagenta", "brightCyan", "brightWhite"];
    for (var i = 0; i < names.length; i++) root.setProperty("--c" + i, next[names[i]]);
    if (term) term.options.theme = next;
  }

  function applyFont(next) {
    font = { size: next.size, family: next.family };
    root.setProperty("--font", next.family);
    root.setProperty("--font-size", next.size + "px");
    if (!term) return;
    term.options.fontFamily = next.family;
    term.options.fontSize = next.size;
    fit();
  }

  // History and scrolling.

  function scrollMax() {
    return Math.max(0, scroller.scrollHeight - scroller.clientHeight);
  }
  function isAtBottom() {
    return scroller.scrollTop >= scrollMax() - 1;
  }
  function report() {
    var v = viewportOf(scroller.scrollTop, scrollMax(), cell.h || 1, historyBox.children.length);
    var key = v.top + "," + v.atBottom;
    if (key === lastViewport) return;
    lastViewport = key;
    send({ type: "viewport", top: v.top, atBottom: v.atBottom });
  }
  function scrollToY(y) {
    scroller.scrollTop = Math.max(0, Math.min(scrollMax(), y));
    report();
  }
  function toBottom() {
    scroller.scrollTop = scrollMax();
    report();
  }

  function rowElement(spans) {
    var row = document.createElement("div");
    for (var i = 0; i < spans.length; i++) {
      var s = spans[i];
      if (!s.c && !s.g && !s.f) {
        row.append(s.t);
        continue;
      }
      var span = document.createElement("span");
      span.textContent = s.t;
      if (s.c) span.style.color = s.c;
      if (s.g) span.style.background = s.g;
      if (s.f) span.className = s.f;
      row.append(span);
    }
    return row;
  }

  function applyHistory(m) {
    var wasAtBottom = isAtBottom();
    if (m.reset) {
      historyBox.replaceChildren();
      marked = null;
    } else if (m.removed > 0) {
      var n = Math.min(m.removed, historyBox.children.length);
      for (var i = 0; i < n; i++) historyBox.firstElementChild.remove();
      // While scrolled up, the rows on screen stay put.
      if (!wasAtBottom) scroller.scrollTop = Math.max(0, scroller.scrollTop - n * (cell.h || 0));
    }
    if (m.rows.length) {
      var rows = document.createDocumentFragment();
      for (var j = 0; j < m.rows.length; j++) rows.append(rowElement(m.rows[j]));
      historyBox.append(rows);
    }
    if (m.cols > 0) root.setProperty("--cols", String(m.cols));
    alternate = m.alternate;
    if (wasAtBottom || alternate) toBottom();
    else report();
  }

  function mark(index) {
    if (marked) marked.classList.remove("mk");
    marked = index >= 0 ? historyBox.children[index] || null : null;
    if (marked) marked.classList.add("mk");
  }

  /** A drag of `px` pixels (> 0 back in time): the history scrolls, else the program gets lines of `unit` pixels. */
  function drag(px, unit) {
    if (alternate || historyBox.children.length === 0) {
      var step = gather(pending, px, unit);
      pending = step.pending;
      if (step.lines !== 0) send({ type: "scroll", lines: step.lines });
      return;
    }
    pending = 0;
    scrollToY(scroller.scrollTop - px);
  }

  scroller.addEventListener("wheel", function (e) {
    e.preventDefault();
    e.stopPropagation();
    var px = e.deltaMode === 1 ? e.deltaY * WHEEL_LINE_PX : e.deltaMode === 2 ? e.deltaY * scroller.clientHeight : e.deltaY;
    drag(-px, WHEEL_LINE_PX);
  }, { capture: true, passive: false });

  var touchY = null;
  scroller.addEventListener("touchstart", function (e) {
    touchY = e.touches.length === 1 ? e.touches[0].clientY : null;
  }, { capture: true, passive: true });
  scroller.addEventListener("touchmove", function (e) {
    if (touchY === null || e.touches.length !== 1) return;
    var y = e.touches[0].clientY;
    var dy = y - touchY;
    touchY = y;
    e.preventDefault();
    drag(dy, cell.h || 16);
  }, { capture: true, passive: false });
  scroller.addEventListener("touchend", function () { touchY = null; }, { capture: true, passive: true });
  scroller.addEventListener("scroll", report);

  // Taps: a link opens (the app decides what is one), else the app focuses.

  function selectedText() {
    var selection = window.getSelection ? window.getSelection() : null;
    return selection ? String(selection) : "";
  }

  /** The UTF-16 index of the character under (x, y) in a history row; -1 past its end. */
  function historyIndex(row, x, y) {
    var text = row.textContent || "";
    var node = null;
    var offset = 0;
    if (document.caretPositionFromPoint) {
      var position = document.caretPositionFromPoint(x, y);
      if (position) { node = position.offsetNode; offset = position.offset; }
    } else if (document.caretRangeFromPoint) {
      var range = document.caretRangeFromPoint(x, y);
      if (range) { node = range.startContainer; offset = range.startOffset; }
    }
    if (!node || !row.contains(node) || node.nodeType !== 3) return -1;
    // The caret falls on the nearer edge of a character: the one under x is before it when x is left of that edge.
    if (offset > 0) {
      var before = document.createRange();
      before.setStart(node, offset - 1);
      before.setEnd(node, offset);
      var rect = before.getBoundingClientRect();
      if (x < rect.right) offset -= 1;
    }
    var walker = document.createTreeWalker(row, NodeFilter.SHOW_TEXT);
    var index = 0;
    for (var t = walker.nextNode(); t && t !== node; t = walker.nextNode()) index += t.nodeValue.length;
    index += offset;
    return index < text.length ? index : -1;
  }

  function tap(x, y) {
    var target = document.elementFromPoint(x, y);
    var row = target && target.closest ? target.closest("#history > div") : null;
    if (row) {
      var col = cell.w ? Math.floor((x - row.getBoundingClientRect().left - 6) / cell.w) : 0;
      send({ type: "tapCell", col: Math.max(0, col), row: -1, text: row.textContent || "", index: historyIndex(row, x, y) });
      return;
    }
    if (!term || !cell.w || !cell.h) return;
    var area = term.element && term.element.querySelector(".xterm-screen");
    if (!area) return;
    var box = area.getBoundingClientRect();
    var c = Math.floor((x - box.left) / cell.w);
    var r = Math.floor((y - box.top) / cell.h);
    if (c < 0 || r < 0 || c >= term.cols || r >= term.rows) return;
    var buffer = term.buffer.active;
    var line = buffer.getLine(buffer.viewportY + r);
    if (!line) return;
    var text = line.translateToString(true);
    var cells = [];
    for (var i = 0; i <= c; i++) {
      var at = line.getCell(i);
      cells.push(at ? [at.getChars(), at.getWidth()] : ["", 1]);
    }
    send({ type: "tapCell", col: c, row: r, text: text, index: utf16Index(cells, text.length) });
  }

  var down = null;
  scroller.addEventListener("pointerdown", function (e) {
    down = e.button === 0 ? { x: e.clientX, y: e.clientY } : null;
  }, true);
  scroller.addEventListener("pointerup", function (e) {
    var start = down;
    down = null;
    if (!start || Math.abs(e.clientX - start.x) > TAP_SLOP_PX || Math.abs(e.clientY - start.y) > TAP_SLOP_PX) return;
    var x = e.clientX;
    var y = e.clientY;
    var at = Date.now();
    // After the terminal's own handling: a hyperlink it activated, a word a double click selected.
    setTimeout(function () {
      if (linkAt >= at || (term && term.hasSelection()) || selectedText()) return;
      tap(x, y);
    }, 0);
  }, true);

  // The clipboard.

  /** The terminal's selection on every copy (the browser's own ⌘C / Ctrl+C, or execCommand). */
  document.addEventListener("copy", function (e) {
    if (!term || !term.hasSelection() || !e.clipboardData) return;
    e.clipboardData.setData("text/plain", term.getSelection());
    e.preventDefault();
    copiedAt = Date.now();
  }, true);

  /** A copy the user asked for: in this page if the browser lets it, else by the app (the page is another origin). */
  function copySelection() {
    if (!term) return;
    var text = term.getSelection();
    if (!text) return;
    var copied = false;
    try {
      copied = document.execCommand("copy");
    } catch (error) {
      copied = false;
    }
    if (!copied) send({ type: "copy", text: text });
  }

  function onKey(event) {
    if (!term) return true;
    var action = keyAction(event, term.hasSelection(), mac);
    if (action.kind === "pass") return true;
    if (action.kind === "native") {
      var at = Date.now();
      // The browser copies next (a `copy` event) or pastes; if it did not copy, copy here.
      setTimeout(function () {
        if (event.key.toLowerCase() === "c" && copiedAt < at) copySelection();
        if (action.clear && term) term.clearSelection();
      }, 0);
      return false;
    }
    event.preventDefault();
    if (action.kind === "copy") {
      copySelection();
      if (action.clear) term.clearSelection();
    }
    return false;
  }

  // Commands from the app.

  function probe() {
    var text = [];
    var drawn = "";
    if (term) {
      var buffer = term.buffer.active;
      for (var i = 0; i < term.rows; i++) {
        var line = buffer.getLine(buffer.viewportY + i);
        text.push(line ? line.translateToString(true) : "");
      }
      var rows = term.element && term.element.querySelector(".xterm-rows");
      drawn = rows ? rows.textContent || "" : "";
    }
    var grid = (term ? term.cols + "x" + term.rows : "no terminal") + " cell " + cell.w + "x" + cell.h + " box " + screen.clientWidth + "x" + screen.clientHeight;
    send({ type: "probe", text: text.join("\n").replace(/\s+$/, ""), drawn: drawn.replace(/\s+$/, ""), history: historyBox.children.length, grid: grid });
  }

  /** Makes the terminal again (after `failed`): a broken one is let go; xterm.js loads again if it never did. Its `size` clears the failure. */
  function retry() {
    if (term) {
      var old = term;
      term = null;
      try {
        old.dispose();
      } catch (error) {
        // Already broken: it goes either way.
      }
    }
    if (typeof window.Terminal === "function" || !xtermScript) {
      create();
      return;
    }
    var script = document.createElement("script");
    script.src = xtermScript.src + (xtermScript.src.indexOf("?") < 0 ? "?" : "&") + "retry=" + Date.now();
    script.onload = create;
    script.onerror = create;
    document.head.append(script);
  }

  function handle(m) {
    if (!m || typeof m.type !== "string") return;
    switch (m.type) {
      case "theme": applyTheme(m.theme); break;
      case "font": applyFont(m); break;
      case "write": if (term) term.write(m.data); break;
      case "reset": if (term) term.reset(); break;
      case "focus": if (term) term.focus(); break;
      case "blur": if (term) term.blur(); break;
      case "history": applyHistory(m); break;
      case "scrollTo": scrollToY(m.row * (cell.h || 0)); break;
      case "scrollToBottom": toBottom(); break;
      case "mark": mark(m.row); break;
      case "retry": retry(); break;
      case "probe": probe(); break;
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
    for (var j = 0; j < batch.length; j++) {
      try {
        handle(batch[j]);
      } catch (error) {
        send({ type: "failed", message: "The terminal page failed (" + batch[j].type + "): " + String((error && error.message) || error) });
      }
    }
  });

  if (window.ResizeObserver) new ResizeObserver(function () { fit(); }).observe(screen);
  else addEventListener("resize", fit);
  // Cells measured with a fallback face are wrong once the real one arrives.
  if (document.fonts && document.fonts.ready) document.fonts.ready.then(function () { if (term) { term.options.fontFamily = font.family; fit(); } });
  if (coarse) coarse.addEventListener("change", function () { send({ type: "pointer", touch: coarse.matches }); });

  create();
  send({ type: "ready", touch: coarse ? coarse.matches : false });
})();
