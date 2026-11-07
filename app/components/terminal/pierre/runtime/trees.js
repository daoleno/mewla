// The file tree: @pierre/trees inside a WebView (or straight in the page on
// web). It renders only; the host owns data and menus; search is Trees' own field.
// Messages in and out are described by ../treeViewerProtocol.ts.
import { FileTree } from "@pierre/trees";
import { LONG_PRESS_MS, decorationParts, movedPastSlop, treeGitStatus } from "./treeModel.ts";

export function mountTreeViewer(container, post) {
  const mount = document.createElement("div");
  mount.style.cssText = "position:absolute;inset:0";
  container.appendChild(mount);

  let style = { vars: {}, dark: true, fontFamily: "monospace", fontSize: 13, itemHeight: 40 };
  let model = null; // last tree contents, kept so a restyle can rebuild
  let tree = null;
  let disposed = false;
  const send = (message) => { if (!disposed) post(message); };

  function applyStyle() {
    for (const [name, value] of Object.entries(style.vars)) container.style.setProperty(name, value);
    container.style.setProperty("--trees-font-family-override", style.fontFamily);
    container.style.setProperty("--trees-font-size-override", `${style.fontSize}px`);
    container.style.background = style.vars["--trees-bg-override"] || "transparent";
    container.style.colorScheme = style.dark ? "dark" : "light";
  }

  function build() {
    if (disposed) return;
    if (tree) { try { tree.cleanUp(); } catch {} tree = null; }
    mount.textContent = "";
    applyStyle();
    if (!model) return;
    const decorations = model.decorations;
    const colors = { added: style.vars["--trees-git-added-color-override"], deleted: style.vars["--trees-git-deleted-color-override"], muted: style.vars["--trees-fg-muted-override"] };
    tree = new FileTree({
      paths: model.paths,
      flattenEmptyDirectories: true,
      initialExpansion: model.expand,
      initialSelectedPaths: model.selected ? [model.selected] : [],
      fileTreeSearchMode: "hide-non-matches",
      search: model.search,
      dragAndDrop: false,
      renaming: false,
      itemHeight: style.itemHeight,
      stickyFolders: false,
      gitStatus: model.gitStatus.map((entry) => ({ path: entry.path, status: treeGitStatus(entry.status) })),
      renderRowDecoration: ({ item }) => {
        const parts = item.kind === "directory" ? null : decorationParts(decorations[item.path], colors);
        return parts ? { text: parts.map((part) => part.text).join(""), parts } : null;
      },
    });
    tree.render({ containerWrapper: mount });
  }

  // Taps and presses are read off the rows so a second tap on the selected
  // file still opens it; selection callbacks only fire on change.
  const rowOf = (event) => {
    for (const node of event.composedPath()) {
      if (node && node.getAttribute && node.getAttribute("data-type") === "item") return node;
    }
    return null;
  };
  const describe = (row) => ({ path: row.getAttribute("data-item-path"), kind: row.getAttribute("data-item-type") === "folder" ? "directory" : "file" });

  let press = null;
  let pressed = false;
  container.addEventListener("click", (event) => {
    if (pressed) { pressed = false; event.preventDefault(); event.stopPropagation(); return; }
    const row = rowOf(event);
    if (!row) return;
    const item = describe(row);
    if (item.kind === "file") send({ type: "open", path: item.path });
  }, true);
  container.addEventListener("touchstart", (event) => {
    const row = rowOf(event);
    if (!row || event.touches.length !== 1) return;
    const touch = event.touches[0];
    const item = describe(row);
    press = { x: touch.clientX, y: touch.clientY, timer: setTimeout(() => { pressed = true; press = null; send({ type: "menu", ...item }); }, LONG_PRESS_MS) };
  }, { passive: true, capture: true });
  const cancelPress = () => { if (press) { clearTimeout(press.timer); press = null; } };
  container.addEventListener("touchmove", (event) => {
    if (press && event.touches[0] && movedPastSlop(press.x, press.y, event.touches[0].clientX, event.touches[0].clientY)) cancelPress();
  }, { passive: true, capture: true });
  container.addEventListener("touchend", cancelPress, { passive: true, capture: true });
  container.addEventListener("touchcancel", cancelPress, { passive: true, capture: true });
  container.addEventListener("contextmenu", (event) => {
    const row = rowOf(event);
    if (!row) return;
    event.preventDefault();
    send({ type: "menu", ...describe(row) });
  }, true);

  function handleOne(message) {
    switch (message.type) {
      case "style":
        style = { vars: message.vars, dark: message.dark, fontFamily: message.fontFamily, fontSize: message.fontSize, itemHeight: message.itemHeight };
        build();
        break;
      case "paths":
        model = { paths: message.paths, gitStatus: message.gitStatus, decorations: message.decorations, expand: message.expand, search: Boolean(message.search), selected: message.selected };
        build();
        break;
      case "select":
        model = model && { ...model, selected: message.path || undefined };
        if (tree) {
          for (const path of tree.getSelectedPaths()) tree.getItem(path)?.deselect();
          if (message.path) tree.getItem(message.path)?.select();
        }
        break;
      case "reveal":
        if (tree) tree.scrollToPath(message.path, { focus: false, offset: "center" });
        break;
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
      cancelPress();
      if (tree) { try { tree.cleanUp(); } catch {} }
      mount.remove();
    },
  };
}
