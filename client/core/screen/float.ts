import { phoneAgentWorking, phoneOwner } from "../phone/setup";
import type { PhoneStatus } from "../phone/status";
import { displayRoute } from "./displayRoute";
import type { AgentDisplay, ScreenAgent, ScreenRoute } from "./screen";

/**
 * The one floating window over the app for every agent screen: Phone use, an agent's Linux display,
 * the Mac's screen. Which screens it may show, the one it shows, what opens a screen's socket, and
 * where the window sits and how big it is. From the Expo app's screenFloatModel.ts and floatGeometry.ts.
 */

/** The kinds of agent screen the one floating window can host. */
export type ScreenKind = "phone" | "display" | "mac";

export interface ScreenSource {
  key: string;
  kind: ScreenKind;
  /** The Worker that uses it, when the App can tell. */
  owner: string | null;
  title: string;
  /** The screen's own size; the shape of the window. */
  width: number;
  height: number;
}

/** A display counts as in use from its agent's last action for this long; once shown it stays while the agent works. */
export const DISPLAY_APPEAR_S = 60;
export const DISPLAY_STAY_S = 15 * 60;

export { phoneOwner };

/** The phone's screen while an agent works it and the screen has a size, or null. */
export function phoneSource(status: PhoneStatus | null, workers: readonly { id: string; name: string }[]): ScreenSource | null {
  if (!status || !phoneAgentWorking(status) || status.w <= 0 || status.h <= 0) return null;
  const agent = status.agent ?? "";
  return { key: "phone", kind: "phone", owner: phoneOwner(agent, workers), title: agent || "Phone", width: status.w, height: status.h };
}

/**
 * The Worker whose page is open: a Session is its Worker, a Work file the Session it was written in
 * (`workFileOwner`); every other page belongs to none.
 */
export function routeOwner(route: { name: string; workerId?: string; id?: string } | null | undefined, workFileOwner: (id: string) => string | undefined): string | null {
  if (route?.name === "session" && route.workerId) return route.workerId;
  if (route?.name === "workFile" && route.id) return workFileOwner(route.id) ?? null;
  return null;
}

/**
 * Whether an agent's display belongs in the window. It appears while its Worker runs and has used it a moment ago;
 * once it is up it stays while the Worker runs, so a pause between actions does not close the view. A person holding
 * it keeps it up; a Stop takes it away.
 */
export function displayShown(display: Pick<AgentDisplay, "stopped" | "idle_s">, state: { running: boolean; shown: boolean; holding: boolean }): boolean {
  if (display.stopped) return false;
  if (state.holding) return true;
  if (!state.running) return false;
  // A daemon that does not say how long it has been idle is read as in use.
  const idle = display.idle_s ?? 0;
  return idle <= (state.shown ? DISPLAY_STAY_S : DISPLAY_APPEAR_S);
}

export function displaySource(display: AgentDisplay, name?: string): ScreenSource {
  return { key: `display:${display.id}`, kind: "display", owner: display.id, title: name || display.id, width: display.width, height: display.height };
}

/** The Mac's screen does not say how big it is until it connects; a laptop's shape stands in. */
const MAC_SHAPE = { width: 1440, height: 900 };

export function macSource(agent: ScreenAgent): ScreenSource {
  return { key: `mac:${agent.worker}`, kind: "mac", owner: agent.worker, title: agent.name || "An agent", ...MAC_SHAPE };
}

/** The page the person is on, as the window needs it. */
export interface FloatPage {
  /** The Worker whose Session or Work file is open, or null on a page that belongs to none (lists, settings). */
  owner: string | null;
  /** The phone's own full view (Phone use) is open. */
  phonePage: boolean;
  /** Screen, the full view of the displays and the Mac, is open. */
  screenPage: boolean;
}

/**
 * The screens the window may show on this page: those of the Worker whose Session or Work file is open (and any whose
 * Worker is unknown), or every one on a page that belongs to no Worker. A screen is never floated over its own full view.
 */
export function floatSources(all: readonly ScreenSource[], page: FloatPage): ScreenSource[] {
  return all.filter((source) => {
    if (source.kind === "phone" ? page.phonePage : page.screenPage) return false;
    return page.owner === null || source.owner === null || source.owner === page.owner;
  });
}

/** The one the window shows: the one the person picked while it is still there, else the first. */
export function pickSource(sources: readonly ScreenSource[], picked: string | null): ScreenSource | null {
  return sources.find((s) => s.key === picked) ?? sources[0] ?? null;
}

/** What opens the live socket for a screen. A Mac's is a watch of that Worker: a viewer that is not takes the screen and ends the agent's turn. */
export interface ScreenTarget {
  watch: boolean;
  worker?: string;
  route?: ScreenRoute;
}

/** Screen's route for the float's Open: an agent's display opens Screen at that agent's card. */
export function screenRoute(worker: string | null): { name: "screen"; worker?: string } {
  return worker ? { name: "screen", worker } : { name: "screen" };
}

export function screenTarget(source: Pick<ScreenSource, "kind" | "owner">): ScreenTarget | null {
  if (source.kind === "display" && source.owner) return { watch: false, route: displayRoute(source.owner) };
  if (source.kind === "mac" && source.owner) return { watch: true, worker: source.owner };
  return null;
}

// Geometry: the same for every kind of screen.

/** Where the floating screen sits. Kept under the Expo app's keys, which the upgrade copies. */
export type FloatCorner = "top-left" | "top-right" | "bottom-left" | "bottom-right";
export const FLOAT_CORNER_KEY = "mewla:phone_float_corner";
export const FLOAT_SIZE_KEY = "mewla:phone_float_size";

/** Bottom-right, above the composer: the chat's text is read from the left, so most of it stays in view. */
export const DEFAULT_FLOAT_CORNER: FloatCorner = "bottom-right";

export function parseFloatCorner(value: string | null): FloatCorner {
  return value === "top-left" || value === "top-right" || value === "bottom-left" || value === "bottom-right" ? value : DEFAULT_FLOAT_CORNER;
}

/** The free rectangle the float may sit in: above the composer and the keyboard, under the header. */
export interface FloatArea {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** Where the float's top-left corner goes for a corner of the free area. */
export function floatOrigin(corner: FloatCorner, area: FloatArea, size: { width: number; height: number }, margin: number) {
  const x = corner.endsWith("left") ? area.left + margin : area.right - margin - size.width;
  const y = corner.startsWith("top") ? area.top + margin : area.bottom - margin - size.height;
  // A tiny area keeps the float on screen rather than off its top or left.
  return { x: Math.max(area.left, x), y: Math.max(area.top, y) };
}

/** The corner nearest the float's centre once the finger lets go. */
export function nearestFloatCorner(center: { x: number; y: number }, area: FloatArea): FloatCorner {
  const horizontal = center.x < (area.left + area.right) / 2 ? "left" : "right";
  const vertical = center.y < (area.top + area.bottom) / 2 ? "top" : "bottom";
  return `${vertical}-${horizontal}`;
}

/** The float's width as a share of the window: where it starts, what a double tap enlarges it to, and how far the person may take it. */
export const FLOAT_DEFAULT = 0.55;
export const FLOAT_LARGE = 0.8;
export const FLOAT_MIN = 0.3;
export const FLOAT_MAX = 0.9;
/** On a wide window the default and the large size stay corner windows, not half the page; a size the person drags to is theirs. */
export const FLOAT_WIDE = 420;
export const FLOAT_WIDE_LARGE = 840;

export function clampFloatFraction(value: number): number {
  return Math.min(FLOAT_MAX, Math.max(FLOAT_MIN, value));
}

/** The size the person left it at, or the default for anything that is not a share in range. */
export function parseFloatFraction(value: string | null): number {
  const parsed = value === null ? Number.NaN : Number(value);
  return Number.isFinite(parsed) && parsed >= FLOAT_MIN && parsed <= FLOAT_MAX ? parsed : FLOAT_DEFAULT;
}

/** A double tap goes between the default and the large size. */
export function toggledFloatFraction(fraction: number): number {
  return Math.abs(fraction - FLOAT_DEFAULT) < 0.01 ? FLOAT_LARGE : FLOAT_DEFAULT;
}

/**
 * The float's size: exactly the screen's proportions, so its picture fills the window with no bars,
 * no stretching and no crop. The default stays under 60% of the window's height, a size the person
 * chose under 85%; a keyboard that takes room gives height first, down to the least width.
 */
export function floatSize(ratio: number, fraction: number, window: { width: number; height: number }, freeHeight?: number) {
  const safe = ratio > 0 ? ratio : 9 / 20;
  const heightShare = Math.abs(fraction - FLOAT_DEFAULT) < 0.01 ? 0.6 : 0.85;
  let width = Math.min(fraction * window.width, heightShare * window.height * safe);
  if (Math.abs(fraction - FLOAT_DEFAULT) < 0.01) width = Math.min(width, FLOAT_WIDE);
  else if (Math.abs(fraction - FLOAT_LARGE) < 0.01) width = Math.min(width, FLOAT_WIDE_LARGE);
  if (freeHeight !== undefined) width = Math.min(width, freeHeight * safe);
  width = Math.max(width, FLOAT_MIN * window.width);
  return { width, height: width / safe };
}

/**
 * The width after a finger moved the corner opposite the float's own corner by (dx, dy): the movement
 * along the diagonal counts, so the proportions stay locked whichever way it is dragged.
 */
export function resizedFloatWidth(startWidth: number, ratio: number, delta: { x: number; y: number }, corner: FloatCorner): number {
  const safe = ratio > 0 ? ratio : 9 / 20;
  const sx = corner.endsWith("left") ? 1 : -1;
  const sy = corner.startsWith("top") ? 1 : -1;
  return startWidth + (sx * delta.x + (sy * delta.y) / safe) / (1 + 1 / (safe * safe));
}
