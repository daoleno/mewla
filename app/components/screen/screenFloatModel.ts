import type { PhoneStatus } from "../../modules/mewla-phone/src";
import type { AgentDisplay } from "../../services/display";
import { displayRoute } from "../../services/displayRoute";
import type { ScreenAgent, ScreenRoute } from "../../services/screen";
import { decodeWorkerRouteId } from "../../services/workerRouteId";
import { phoneAgentWorking } from "../phone/phoneSetup";

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

export const PHONE_ROUTE = "/phone";
export const SCREEN_ROUTE = "/screen";

/** Phone shows its screen on the Phone page; display and Mac screens on Screen. */
export function homeRoute(kind: ScreenKind): string {
  return kind === "phone" ? PHONE_ROUTE : SCREEN_ROUTE;
}

function onRoute(pathname: string, route: string): boolean {
  return pathname === route || pathname.startsWith(`${route}/`);
}

/**
 * The Worker whose Session or Work is on screen, or null on a page that belongs to none (lists, settings).
 * `workOwner` reads the Worker a Work item runs on.
 */
export function routeOwner(pathname: string, workOwner: (workId: string) => string | undefined): string | null {
  const parts = pathname.split("/").filter(Boolean);
  if (parts.length !== 2) return null;
  try {
    if (parts[0] === "terminal") return decodeWorkerRouteId(parts[1]);
    if (parts[0] === "work") return workOwner(decodeURIComponent(parts[1])) ?? null;
  } catch {
    return null;
  }
  return null;
}

/** Phone only names the agent (its name, else its id): match it to a Worker, or leave it ownerless. */
export function phoneOwner(agent: string, workers: readonly { id: string; name: string }[]): string | null {
  const byId = workers.find((w) => w.id === agent);
  if (byId) return byId.id;
  const byName = workers.filter((w) => w.name === agent);
  return byName.length === 1 ? byName[0].id : null;
}

export function phoneSource(status: PhoneStatus | null, workers: readonly { id: string; name: string }[]): ScreenSource | null {
  if (!status || !phoneAgentWorking(status) || status.w <= 0 || status.h <= 0) return null;
  const agent = status.agent ?? "";
  return { key: "phone", kind: "phone", owner: phoneOwner(agent, workers), title: agent || "Phone", width: status.w, height: status.h };
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

/**
 * The screens the window may show on this page: those of the Worker whose Session or Work is open (and any whose
 * Worker is unknown), or every one on a page that belongs to no Worker. A screen is never floated over its own full view.
 */
export function floatSources(all: readonly ScreenSource[], page: { pathname: string; owner: string | null }): ScreenSource[] {
  return all.filter((source) => {
    if (onRoute(page.pathname, homeRoute(source.kind))) return false;
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

export function screenTarget(source: Pick<ScreenSource, "kind" | "owner">): ScreenTarget | null {
  if (source.kind === "display" && source.owner) return { watch: false, route: displayRoute(source.owner) };
  if (source.kind === "mac" && source.owner) return { watch: true, worker: source.owner };
  return null;
}
