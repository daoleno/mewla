import { withoutSessionId } from "../sessions/listRow";
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
  /** The agent asks the person to take over: that request, by an id new for each one, or null. */
  asking?: string | null;
}

/** A display counts as in use from its agent's last action for this long; once shown it stays while the agent works. */
export const DISPLAY_APPEAR_S = 60;
export const DISPLAY_STAY_S = 15 * 60;

export { phoneOwner };

/** The phone's screen while an agent works it and the screen has a size, or null; `request` names the handoff it asks for now. */
export function phoneSource(status: PhoneStatus | null, workers: readonly { id: string; name: string }[], request: string | null = null): ScreenSource | null {
  if (!status || !phoneAgentWorking(status) || status.w <= 0 || status.h <= 0) return null;
  const agent = status.agent ?? "";
  return { key: "phone", kind: "phone", owner: phoneOwner(agent, workers), title: agent || "Phone", width: status.w, height: status.h, asking: status.handoff ? request ?? status.handoff : null };
}

/**
 * The id of the take-over request the agent makes now: kept while it asks the same thing, new for a new ask (another
 * text, or the same after none), null while it asks nothing. `fresh` mints one.
 */
export function requestId(previous: { text: string; id: string } | null, asked: string | null, fresh: () => string): { text: string; id: string } | null {
  if (!asked) return null;
  return previous?.text === asked ? previous : { text: asked, id: fresh() };
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
  return { key: `display:${display.id}`, kind: "display", owner: display.id, title: withoutSessionId(name || display.name) || display.id, width: display.width, height: display.height };
}

/** The Mac's screen does not say how big it is until it connects; a laptop's shape stands in. */
const MAC_SHAPE = { width: 1440, height: 900 };

export function macSource(agent: ScreenAgent): ScreenSource {
  return { key: `mac:${agent.worker}`, kind: "mac", owner: agent.worker, title: withoutSessionId(agent.name) || "An agent", ...MAC_SHAPE };
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
 * Worker is unknown), or every one on a page that belongs to no Worker. A screen is never floated over its own full view,
 * nor one whose window closed itself (`closed`, by `key`), nor one the person hid (`hidden`, by `hideKey`) unless its
 * agent asks them to take over: that request brings it back, until they hide it during that same request
 * (`hiddenDuring`, the request each was hidden in).
 */
export function floatSources(
  all: readonly ScreenSource[],
  page: FloatPage,
  hidden: ReadonlySet<string> = new Set(),
  closed: ReadonlySet<string> = new Set(),
  hiddenDuring: ReadonlyMap<string, string> = new Map(),
): ScreenSource[] {
  return all.filter((source) => {
    if (source.kind === "phone" ? page.phonePage : page.screenPage) return false;
    if (closed.has(source.key)) return false;
    const key = hideKey(source);
    if (hidden.has(key) && !(source.asking && hiddenDuring.get(key) !== source.asking)) return false;
    return page.owner === null || source.owner === null || source.owner === page.owner;
  });
}

// Hiding: the person closes the window for one Worker; it stays closed for that Worker until they open the screen's
// own page (Screen, or Phone use for the phone). Another Worker's screen still floats by itself.

/** What a hidden window is kept under: a display's or the Mac's is its Worker's key, the phone's the agent working it. */
export function hideKey(source: Pick<ScreenSource, "key" | "kind" | "owner" | "title">): string {
  return source.kind === "phone" ? `phone:${source.owner ?? source.title}` : source.key;
}

/** Where the hidden keys are kept, one list per computer. */
export const FLOAT_HIDDEN_KEY = "mewla:screen_float_hidden";
/** The most hidden keys kept per computer: Worker ids only grow, so the oldest go. */
export const FLOAT_HIDDEN_MAX = 50;

/** The stored list read back: only strings, at most the last FLOAT_HIDDEN_MAX. */
export function parseHiddenKeys(value: string | null): string[] {
  if (!value) return [];
  try {
    const parsed: unknown = JSON.parse(value);
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string").slice(-FLOAT_HIDDEN_MAX) : [];
  } catch {
    return [];
  }
}

/** The list with `key` hidden, newest last. */
export function withHidden(keys: readonly string[], key: string): string[] {
  return [...keys.filter((item) => item !== key), key].slice(-FLOAT_HIDDEN_MAX);
}

/**
 * Opening a screen's own page brings its windows back: Screen every display's and the Mac's (its stage switches between
 * agents), Phone use the phone's. Returns the same list when nothing changes.
 */
export function unhiddenOn(keys: readonly string[], page: Pick<FloatPage, "screenPage" | "phonePage">): readonly string[] {
  const kept = keys.filter((key) => !(key.startsWith("phone:") ? page.phonePage : page.screenPage));
  return kept.length === keys.length ? keys : kept;
}

/**
 * After the window's live view ended while the app is in front: how long until it tries again, or null when the window
 * closes itself instead of sitting there dark. A view that was live tries again a few times, so a dropped link recovers
 * by itself; one that never came up (the video cannot reach this device) gets one more try; a computer too old for this
 * app, none. `failures` counts the attempts that ended in a row, this one included.
 */
export const FLOAT_RETRY_MS = [1_000, 3_000, 8_000] as const;
export function floatRetryDelay(failures: number, state: { everLive: boolean; outdated: boolean }): number | null {
  if (state.outdated || failures < 1) return null;
  const tries = state.everLive ? FLOAT_RETRY_MS.length : 1;
  return failures <= tries ? FLOAT_RETRY_MS[failures - 1] : null;
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
export function screenRoute(worker: string | null, takeOver = false): { name: "screen"; worker?: string; takeOver?: boolean } {
  return worker ? (takeOver ? { name: "screen", worker, takeOver } : { name: "screen", worker }) : { name: "screen" };
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

/**
 * On a wide window the float docks in a strip at the right of the right pane, beside the page, so it
 * covers none of it (SPEC §14.2, decided 11-13 after the owner's float lay over the thread's top right):
 * the page keeps its widest column (a thread's 760) left of the strip, and the strip takes the rest, up
 * to 480 with 16 on each side. A pane too narrow for a 240 strip beside that column keeps the float over
 * the pane, in its corner, as on a phone. Returns the strip's whole width (gaps included), 0 for none.
 */
export const DOCK = { column: 760, gap: 16, min: 240, max: 480 } as const;
export function screenDockWidth(stackWidth: number): number {
  const width = Math.min(DOCK.max, stackWidth - DOCK.column - 2 * DOCK.gap);
  return width >= DOCK.min ? width + 2 * DOCK.gap : 0;
}

/** Docked, only top and bottom remain: the strip has one side. */
export function dockedCorner(corner: FloatCorner): FloatCorner {
  return corner.startsWith("top") ? "top-right" : "bottom-right";
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
