// The diff and file reader: @pierre/diffs inside a WebView (or straight in the
// page on web). It renders only; the host owns chrome, data and clipboard.
// Messages in and out are described by ../diffViewerProtocol.ts.
import { CodeView, parsePatchFiles, registerCustomCSSVariableTheme } from "@pierre/diffs";
import { applyColorScheme } from "./colorScheme.js";
import { findMatches, hunkStarts, stepHunk, stepIndex } from "./diffNavigation.ts";

const THEME = "mewla";

// The diff grid is laid out with CSS subgrid (Safari 16, Chromium 117), the
// newest feature the reader cannot work around; an older WebView says so
// instead of drawing a broken grid.
function requireSubgrid() {
  if (!CSS.supports("grid-template-columns", "subgrid")) throw new Error("This WebView is too old to show diffs. Update Android System WebView.");
}
registerCustomCSSVariableTheme(THEME, {}, true);

// Sections of one file share its name; the blob ids on the patch's `index` line
// tell the staged and unstaged halves apart.
const sectionKey = (fileDiff) => `${fileDiff.name}\0${fileDiff.prevObjectId || ""}\0${fileDiff.newObjectId || ""}`;

export function mountDiffViewer(container, post) {
  const root = document.createElement("div");
  root.style.cssText = "position:absolute;inset:0;overflow-y:auto;overflow-x:hidden;overscroll-behavior:contain;-webkit-overflow-scrolling:touch";
  container.appendChild(root);

  let style = { vars: {}, dark: true, fontFamily: "monospace", fontSize: 12 };
  let layout = { split: false, wrap: true, headers: true, inset: 0 };
  let current = null; // { key, kind, items, sections, expandable, labels }
  let view = null;
  let matches = [];
  let query = "";
  let matchIndex = -1;
  let hunkIndex = -1;
  const positions = new Map();
  const pendingFiles = new Map();
  let disposed = false;

  const send = (message) => { if (!disposed) post(message); };

  function applyStyle() {
    const s = container.style;
    for (const [name, value] of Object.entries(style.vars)) s.setProperty(name, value);
    s.setProperty("--diffs-font-family", style.fontFamily);
    s.setProperty("--diffs-font-size", `${style.fontSize}px`);
    s.setProperty("--diffs-line-height", `${Math.round(style.fontSize * 1.6)}px`);
    s.setProperty("--diffs-tab-size", "4");
    s.setProperty("--diffs-gap-block", "0px");
    applyColorScheme(container, style.dark);
    container.style.background = style.vars["--diffs-background"] || "transparent";
    root.style.background = container.style.background;
  }

  function options() {
    const sectionLabel = (fileDiff) => current?.labels.get(sectionKey(fileDiff));
    return {
      theme: { dark: THEME, light: THEME },
      themeType: style.dark ? "dark" : "light",
      diffStyle: layout.split ? "split" : "unified",
      overflow: layout.wrap ? "wrap" : "scroll",
      lineDiffType: "word-alt",
      diffIndicators: "bars",
      // A file with several sections needs its header to tell them apart.
      disableFileHeader: !layout.headers && !(current && current.labels.size),
      hunkSeparators: current?.expandable ? "line-info" : "simple",
      stickyHeaders: true,
      layout: { paddingTop: 0, paddingBottom: 24 + layout.inset, gap: 0 },
      loadDiffFiles: (fileDiff) => requestFiles(fileDiff),
      renderHeaderPrefix: (fileDiff) => {
        const label = sectionLabel(fileDiff);
        if (!label) return null;
        const el = document.createElement("span");
        el.textContent = label;
        el.style.cssText = "font-size:11px;opacity:.7;margin-right:6px;text-transform:uppercase;letter-spacing:.04em";
        return el;
      },
    };
  }

  function requestFiles(fileDiff) {
    const id = current?.expandable?.get(fileDiff.name);
    if (!id) return Promise.reject(new Error("Unavailable"));
    return new Promise((resolve, reject) => {
      pendingFiles.set(id, { resolve, reject, name: fileDiff.name });
      send({ type: "needFiles", id });
    });
  }

  function build() {
    if (disposed) return;
    if (view) { try { view.cleanUp(); } catch {} view = null; }
    root.textContent = "";
    applyStyle();
    if (!current) return;
    view = new CodeView(options());
    view.setup(root);
    view.setItems(current.items);
    view.render(true);
    const saved = positions.get(current.key);
    if (saved) root.scrollTop = saved;
    const started = performance.now();
    const key = current.key;
    const poll = () => {
      if (disposed || !current || current.key !== key) return;
      if (view.getRenderedItems().length > 0 || current.items.length === 0 || performance.now() - started > 8000) {
        send({ type: "loaded", key, hunks: current.hunks, ms: Math.round(performance.now() - started) });
        return;
      }
      requestAnimationFrame(poll);
    };
    poll();
  }

  function remember() {
    if (current) positions.set(current.key, root.scrollTop);
  }

  function showDiff(message) {
    remember();
    const items = [];
    const sections = [];
    const labels = new Map();
    const expandable = new Map();
    let hunks = 0;
    try {
      message.sections.forEach((section, index) => {
        const files = parsePatchFiles(section.patch, { throwOnError: true }).flatMap((patch) => patch.files);
        files.forEach((fileDiff, fileIndex) => {
          const id = `${section.id}:${fileIndex}`;
          items.push({ id, type: "diff", fileDiff, version: 1 });
          sections.push({ id, hunks: fileDiff.hunks, additionLines: fileDiff.additionLines, deletionLines: fileDiff.deletionLines });
          hunks += fileDiff.hunks.length;
          if (section.label) labels.set(sectionKey(fileDiff), section.label);
          if (section.expandable) expandable.set(fileDiff.name, section.id);
        });
      });
    } catch (error) {
      current = null;
      send({ type: "error", key: message.key, message: String(error && error.message || error) });
      return;
    }
    current = { key: message.key, kind: "diff", items, sections, labels, expandable: expandable.size ? expandable : null, hunks };
    matches = []; query = ""; matchIndex = -1; hunkIndex = -1;
    build();
  }

  function showFile(message) {
    remember();
    current = {
      key: message.key,
      kind: "file",
      items: [{ id: "file", type: "file", file: { name: message.path, contents: message.contents }, version: 1 }],
      sections: [], labels: new Map(), expandable: null, hunks: 0,
    };
    matches = []; query = ""; matchIndex = -1; hunkIndex = -1;
    build();
  }

  function scrollToLine(ref, align) {
    if (!view || !ref) return;
    view.scrollTo({ type: "line", id: ref.id, lineNumber: ref.line, side: ref.side, align, behavior: "instant" });
  }

  function find(next) {
    query = next;
    matches = current ? findMatches(current.sections, next) : [];
    matchIndex = -1;
    if (view) view.clearSelectedLines();
    send({ type: "find", query, count: matches.length, index: -1 });
  }

  function seekMatch(direction) {
    if (!matches.length) return;
    matchIndex = stepIndex(matchIndex, matches.length, direction);
    const ref = matches[matchIndex];
    scrollToLine(ref, "center");
    if (view) view.setSelectedLines({ id: ref.id, range: { start: ref.line, end: ref.line, side: ref.side } });
    send({ type: "find", query, count: matches.length, index: matchIndex });
  }

  function seekHunk(direction) {
    if (!current) return;
    const starts = hunkStarts(current.sections);
    hunkIndex = stepHunk(hunkIndex, starts.length, direction);
    scrollToLine(starts[hunkIndex], "start");
  }

  function handleOne(message) {
    switch (message.type) {
      case "style":
        style = { vars: message.vars, dark: message.dark, fontFamily: message.fontFamily, fontSize: message.fontSize };
        remember();
        build();
        break;
      case "layout":
        layout = { split: message.split, wrap: message.wrap, headers: message.headers, inset: message.inset || 0 };
        remember();
        build();
        break;
      case "diff": requireSubgrid(); showDiff(message); break;
      case "file": requireSubgrid(); showFile(message); break;
      case "clear": remember(); current = null; build(); break;
      case "files": {
        const pending = [...pendingFiles.entries()].find(([id]) => id === message.id);
        if (!pending) break;
        pendingFiles.delete(pending[0]);
        const { resolve, reject, name } = pending[1];
        if (message.oldContents === null && message.newContents === null) { reject(new Error("Unavailable")); break; }
        resolve(message.oldContents === null
          ? { oldFile: null, newFile: { name, contents: message.newContents } }
          : { oldFile: { name: message.oldName || name, contents: message.oldContents }, newFile: { name, contents: message.newContents } });
        break;
      }
      case "find": find(message.query); break;
      case "seek": message.target === "hunk" ? seekHunk(message.direction) : seekMatch(message.direction); break;
      case "top": root.scrollTop = 0; break;
    }
  }

  applyStyle();
  return {
    handle(batch) {
      for (const message of batch) {
        try { handleOne(message); } catch (error) { send({ type: "error", message: String(error && error.message || error) }); }
      }
    },
    destroy() {
      disposed = true;
      if (view) { try { view.cleanUp(); } catch {} }
      root.remove();
    },
  };
}
