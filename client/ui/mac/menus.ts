import { effect } from "nsh";
import { focusedWindow, Menu } from "nsh/desktop";
import type { PlaceKey } from "../../core/web/places";
import { runDesktopCommand } from "../shell/desktop/Overlays";
import { openMacSettings } from "./MacSettings";
import { openPairPhone } from "./PairPhone";
import { detachPerch, openPerch } from "./Perch";

const go = (key: PlaceKey) => () => runDesktopCommand({ kind: "go", key });

/**
 * The menu bar's menus (DESIGN-MAC.md §2.3): places live in Go, not in a sidebar. Each item runs what the
 * window's keys and ⌘K palette run.
 */
export function startMacMenus(perchShortcut: () => string | null): void {
  effect(() => {
    Menu.setApplicationMenu([
      {
        label: "Mewla",
        submenu: [
          { role: "about" },
          { type: "separator" },
          { label: "Settings…", shortcut: "mod+,", onSelect: openMacSettings },
          { type: "separator" },
          { role: "hide" },
          { role: "hideOthers" },
          { role: "showAll" },
          { type: "separator" },
          { role: "quit" },
        ],
      },
      {
        label: "File",
        submenu: [
          { label: "New Session", shortcut: "mod+n", onSelect: () => runDesktopCommand({ kind: "new-session" }) },
          { label: "New Chat", shortcut: "mod+shift+n", onSelect: () => runDesktopCommand({ kind: "new-chat" }) },
          { label: "Pair a Phone…", onSelect: () => openPairPhone() },
          { type: "separator" },
          { label: "Close Window", shortcut: "mod+w", onSelect: () => focusedWindow()?.close() },
        ],
      },
      {
        label: "Edit",
        submenu: [{ role: "undo" }, { role: "redo" }, { type: "separator" }, { role: "cut" }, { role: "copy" }, { role: "paste" }, { role: "selectAll" }],
      },
      {
        label: "View",
        submenu: [
          { label: "Brain's Terminal", shortcut: "mod+shift+t", onSelect: () => runDesktopCommand({ kind: "brain-terminal" }) },
          { type: "separator" },
          { role: "toggleFullScreen" },
        ],
      },
      {
        label: "Go",
        submenu: [
          { label: "Brain", shortcut: "mod+1", onSelect: go("brain") },
          { label: "All Sessions", shortcut: "mod+2", onSelect: go("sessions") },
          { label: "Calendar", shortcut: "mod+3", onSelect: go("calendar") },
          { label: "Screen", shortcut: "mod+4", onSelect: go("screen") },
          { label: "This Mac", shortcut: "mod+5", onSelect: go("computer") },
          { type: "separator" },
          { label: "Extensions", onSelect: go("extensions") },
          { label: "Stats", onSelect: go("stats") },
          { label: "Resources", onSelect: go("resources") },
          { type: "separator" },
          { label: "Search or Run…", shortcut: "mod+k", onSelect: () => runDesktopCommand({ kind: "palette" }) },
        ],
      },
      {
        label: "Window",
        submenu: [
          { label: "Quick Chat", shortcut: perchShortcut() ?? undefined, onSelect: () => openPerch() },
          { label: "Detach Quick Chat", onSelect: detachPerch },
          { type: "separator" },
          { role: "minimize" },
          { role: "zoom" },
        ],
      },
    ]);
  });
}
