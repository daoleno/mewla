import { isAgentExtensionsScope } from "../../core/extensions/agentExtensionsModel";
import type { PlacePage, WebPlace } from "../../core/web/places";
import type { Route } from "./routes";

/** The computer page's children that are places of their own (the palette's Go to rows). */
const PLACE_OF_ROUTE: Partial<Record<Route["name"], Exclude<WebPlace["key"], "session" | "work">>> = {
  allSessions: "sessions",
  calendar: "calendar",
  item: "calendar",
  run: "calendar",
  computer: "computer",
  settings: "settings",
  screen: "screen",
  stats: "stats",
  resources: "resources",
  friends: "friends",
  extensions: "extensions",
};

/** The page a route is when it has a URL of its own (parity #47). */
function placePageOf(route: Route): PlacePage | null {
  switch (route.name) {
    case "settings-computer": case "settings-license": return { name: route.name, id: route.id };
    case "friends-post": case "friends-person": case "extensions-service": return { name: route.name, id: route.id };
    case "extensions-tools": return { name: route.name, service: route.service, ...(route.account ? { account: route.account } : {}) };
    case "extensions-agent": return { name: route.name, scope: route.scope };
    case "workFile": return { name: route.name, id: route.id };
    case "settings-telegram": case "settings-providers": case "settings-agents": case "settings-friends": case "settings-iphone": case "settings-phone":
    case "settings-virtual-screen": case "settings-pet": case "settings-licenses":
    case "friends-drafts": case "friends-notifications": case "friends-bookmarks": case "friends-blocked":
      return { name: route.name };
    default: return null;
  }
}

function placeKeyOf(name: string): Exclude<WebPlace["key"], "session" | "work"> | null {
  const direct = PLACE_OF_ROUTE[name as Route["name"]];
  if (direct) return direct;
  if (name.startsWith("settings-")) return "settings";
  if (name.startsWith("extensions-")) return "extensions";
  if (name.startsWith("friends-")) return "friends";
  if (name.startsWith("stats-")) return "stats";
  return null;
}

/**
 * Where the stack is, as a URL names it: the topmost page with a place (a Session, a Settings
 * page, the Calendar…). A page without one (a Git review, a file) keeps the place under it.
 */
export function placeOfRoutes(routes: readonly Route[]): WebPlace {
  for (let i = routes.length - 1; i >= 0; i -= 1) {
    const route = routes[i]!;
    if (route.name === "session") return { key: "session", workerId: route.workerId };
    if (route.name === "workFile") return { key: "work", page: { name: "workFile", id: route.id } };
    if (route.name === "workDetail") return { key: "work", page: { name: "workDetail", id: route.workId } };
    const key = placeKeyOf(route.name);
    if (!key) continue;
    const page = placePageOf(route);
    return page && (key === "settings" || key === "friends" || key === "extensions") ? { key, page } : { key };
  }
  return { key: "brain" };
}

/** The pages a place opens above home: the natural stack, so Back leads somewhere (SPEC §3.8). */
export function routesOfPlace(place: WebPlace): Route[] {
  const page = "page" in place ? place.page : undefined;
  switch (place.key) {
    case "brain": return [];
    case "work":
      if (page?.name === "workFile") return [{ name: "workFile", id: page.id }];
      return page?.name === "workDetail" ? [{ name: "workDetail", workId: page.id }] : [];
    case "sessions": return [{ name: "allSessions" }];
    case "session": return [{ name: "session", workerId: place.workerId }];
    case "calendar": return [{ name: "calendar" }];
    case "computer": return [{ name: "computer" }];
    default: return [{ name: "computer" }, { name: place.key }, ...(page ? pagesUnder(page) : [])];
  }
}

function pagesUnder(page: PlacePage): Route[] {
  switch (page.name) {
    case "workFile": case "workDetail": return [];
    case "settings-license": return [{ name: "settings-licenses" }, page];
    case "extensions-agent": return [{ name: "extensions-agent", scope: isAgentExtensionsScope(page.scope) ? page.scope : "shared" }];
    default: return [page as Route];
  }
}
