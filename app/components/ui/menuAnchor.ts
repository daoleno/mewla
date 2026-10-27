/** A rectangle in window coordinates. */
export interface AnchorRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export const MENU_POPOVER_WIDTH = 280;
const GAP = 6;
const MARGIN = 8;

/**
 * Where a menu opens beside the control that opened it: below it, aligned to
 * the control's nearer edge, and above it when there is no room below.
 */
export function menuPopoverPosition(
  anchor: AnchorRect,
  menuHeight: number,
  viewport: { width: number; height: number },
  menuWidth = MENU_POPOVER_WIDTH,
): { left: number; top: number } {
  const opensLeftward = anchor.x + anchor.width / 2 > viewport.width / 2;
  const preferredLeft = opensLeftward ? anchor.x + anchor.width - menuWidth : anchor.x;
  const left = Math.min(Math.max(MARGIN, preferredLeft), viewport.width - menuWidth - MARGIN);
  const below = anchor.y + anchor.height + GAP;
  const fitsBelow = below + menuHeight <= viewport.height - MARGIN;
  const above = anchor.y - GAP - menuHeight;
  const top = fitsBelow || above < MARGIN ? Math.min(below, Math.max(MARGIN, viewport.height - MARGIN - menuHeight)) : above;
  return { left, top };
}

let lastTrigger: { element: Element; at: number } | null = null;
let listening = false;

/** Remember which control a click or key press last landed on (web only). */
function ensureTriggerTracking() {
  if (listening || typeof document === "undefined") return;
  listening = true;
  const remember = (event: Event) => {
    const target = event.target as Element | null;
    const control = target?.closest?.("button,[role=button],[role=tab]");
    if (control) lastTrigger = { element: control, at: Date.now() };
  };
  document.addEventListener("pointerdown", remember, true);
  document.addEventListener("keydown", remember, true);
}

if (typeof document !== "undefined") ensureTriggerTracking();

/** The control that just opened a menu, if one was used in the last second. */
export function recentTriggerRect(now = Date.now()): AnchorRect | null {
  if (!lastTrigger || now - lastTrigger.at > 1000 || !lastTrigger.element.isConnected) return null;
  const rect = lastTrigger.element.getBoundingClientRect();
  return { x: rect.left, y: rect.top, width: rect.width, height: rect.height };
}
