import { computed, signal, Storage, windowSize } from "nsh";
import { clampPaneWidth, isWide, PANE_WIDTH, PANE_WIDTH_KEY, showsListDetail, storedPaneWidth } from "../../core/web/places";

/**
 * The wide layout (soft-final SPEC §14, client/DESIGN.md "The wide layout"): from a 1024 pt
 * window the tray is a left pane beside the one stack, on every device: a browser, a tablet in
 * landscape, an open foldable. One tree either way: crossing 1024 only adds or removes the pane.
 */
export const wide = computed(() => isWide(windowSize().width));

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

/** A list page shows the item it opens beside it (Git review › diff, Files › file): wide, with a right pane of 880 pt or more. */
export const listDetail = computed(() => showsListDetail({ wide: wide(), paneWidth: rightPaneWidth() || windowSize().width - (wide() ? paneWidth() : 0) }));
