import { computed, safeArea, signal, Storage, windowSize } from "nsh";
import { screenDockWidth } from "../../core/screen/float";
import { clampPaneWidth, nextWide, PANE_WIDTH, PANE_WIDTH_KEY, showsListDetail, storedPaneWidth } from "../../core/web/places";

/**
 * The window's width less what the system draws at its sides (the iPhone Duo's 84 pt side bar, a phone's cutout
 * on its side): the room the panes share. The shell pads its row by the same insets.
 */
export const usableWidth = computed(() => windowSize().width - safeArea().left - safeArea().right);

/**
 * The wide layout (soft-final SPEC §14, client/DESIGN.md "The wide layout"): from 1024 pt of usable width, or 760
 * on its side (an open foldable held like a book), the tray is a left pane beside the one stack, on every device:
 * a browser, a tablet in landscape, an open iPhone Duo or Android foldable. One tree either way: crossing the gate
 * (a resize, a turn, a fold) only adds or removes the pane.
 */
let lastWide: { wide: boolean; width: number } | null = null;
export const wide = computed(() => {
  const width = usableWidth();
  // A height-only shrink at the same width (Android 14 resizing over the keyboard) keeps the pane (core/web/places.ts nextWide).
  const now = nextWide(lastWide, { width, height: windowSize().height });
  lastWide = { wide: now, width };
  return now;
});

/** The left pane's width: dragged, stepped or reset on its handle, kept on this device. */
export const paneWidth = signal<number>(PANE_WIDTH.initial);
let paneWidthRead = false;
/** Reads the saved width once (the shell's first render). */
export function loadPaneWidth(): void {
  if (paneWidthRead) return;
  paneWidthRead = true;
  void Storage.getItem(PANE_WIDTH_KEY).then((raw) => paneWidth.set(storedPaneWidth(raw))).catch(() => {});
}
export function setPaneWidth(value: number): void {
  const width = clampPaneWidth(value);
  paneWidth.set(width);
  void Storage.setItem(PANE_WIDTH_KEY, String(width)).catch(() => {});
}

/** The right pane's width (the whole window on a phone), as laid out: what list + detail follows. */
export const rightPaneWidth = signal(0);

/** The one stack's width: the right pane as laid out, else the window less the tray's pane. */
export const stackWidth = computed(() => rightPaneWidth() || usableWidth() - (wide() ? paneWidth() : 0));

/** The floating screen's strip at the right of the right pane on wide (core/screen/float.ts screenDockWidth); 0 where it floats over the page. */
export const screenDock = computed(() => (wide() ? screenDockWidth(usableWidth() - paneWidth()) : 0));

/** How many floating screens are up (0 or 1): while one is, a docked strip is kept free of the page. */
export const floatsShown = signal(0);

/** A list page shows the item it opens beside it (Git review › diff, Files › file): wide, with a right pane of 880 pt or more. */
export const listDetail = computed(() => showsListDetail({ wide: wide(), paneWidth: stackWidth() }));
