/**
 * The room a one-column header of Mewla for Mac leaves for the traffic lights (DESIGN-MAC.md w-small): 84 from the
 * window's edge while they sit in that row (the window under 1024, its hidden title bar `lightsRow` high), else none
 * (two panes keep them in the left pane; full screen and the other clients have no such row).
 */
export function lightsLead(wide: boolean, lightsRow: number): number {
  return !wide && lightsRow > 0 ? 84 : 0;
}
