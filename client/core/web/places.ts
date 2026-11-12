/**
 * The desktop layout's places: the sidebar rows, the URL of each place, the
 * browser tab's title, and the sidebar's width. Pure, so the shell and the
 * page host agree without a browser (client/DESIGN.md "Desktop web").
 */

/** From this width a non-phone window gets the desktop layout; below it the phone's one stack. */
export const DESKTOP_MIN_WIDTH = 1024;

/** The one gate for the desktop layout: not a phone build, and at least 1024 pt wide. */
export function isDesktopLayout(input: { mobile: boolean; width: number }): boolean {
  return !input.mobile && input.width >= DESKTOP_MIN_WIDTH;
}

/** A sidebar row. */
export type DesktopPlaceKey =
  | "brain"
  | "sessions"
  | "calendar"
  | "extensions"
  | "screen"
  | "stats"
  | "resources"
  | "friends"
  | "settings";

export interface SidebarRow {
  key: DesktopPlaceKey;
  label: string;
}

const ROWS: readonly SidebarRow[] = [
  { key: "brain", label: "Brain" },
  { key: "sessions", label: "Sessions" },
  { key: "calendar", label: "Calendar" },
  { key: "extensions", label: "Extensions" },
  { key: "screen", label: "Screen" },
  { key: "stats", label: "Stats" },
  { key: "resources", label: "Resources" },
  { key: "friends", label: "Friends" },
  { key: "settings", label: "Settings" },
];

/** The sidebar, top to bottom. Friends is native-only (hidden on the web, as before) and a Labs switch. */
export function sidebarRows(options: { friends: boolean }): SidebarRow[] {
  return ROWS.filter((row) => row.key !== "friends" || options.friends);
}

export function placeLabel(key: DesktopPlaceKey): string {
  return ROWS.find((row) => row.key === key)?.label ?? key;
}

/**
 * A page under a place that has a URL of its own, as the Expo app's routes did: a Settings page
 * (`/settings/computer?id=…`), a Friends post or person, a Plugins service, its tools or a tool's
 * extensions, a Work file (`/work/<id>`). The values are the shell's own routes.
 */
export type PlacePage =
  | { name: "settings-computer"; id: string }
  | { name: "settings-telegram" }
  | { name: "settings-providers" }
  | { name: "settings-agents" }
  | { name: "settings-friends" }
  | { name: "settings-iphone" }
  | { name: "settings-phone" }
  | { name: "settings-virtual-screen" }
  | { name: "settings-pet" }
  | { name: "settings-licenses" }
  | { name: "settings-license"; id: string }
  | { name: "friends-post"; id: string }
  | { name: "friends-person"; id: string }
  | { name: "friends-drafts" }
  | { name: "friends-notifications" }
  | { name: "friends-bookmarks" }
  | { name: "friends-blocked" }
  | { name: "extensions-service"; id: string }
  | { name: "extensions-tools"; service: string; account?: string }
  | { name: "extensions-agent"; scope: string }
  | { name: "workFile"; id: string };

/** A place with a URL: a sidebar row, the computer page, a Work file, or one Session of the current computer; `page`, a page under it. */
export type WebPlace =
  | { key: Exclude<DesktopPlaceKey, "sessions">; page?: PlacePage }
  | { key: "sessions" }
  | { key: "computer" }
  | { key: "work"; page: PlacePage }
  | { key: "session"; workerId: string };

/** Settings' pages by their path segment (the Expo app's `app/settings/*.tsx`). */
const SETTINGS_PAGES: Record<string, PlacePage> = {
  telegram: { name: "settings-telegram" },
  "model-providers": { name: "settings-providers" },
  agents: { name: "settings-agents" },
  friends: { name: "settings-friends" },
  iphone: { name: "settings-iphone" },
  phone: { name: "settings-phone" },
  "virtual-screen": { name: "settings-virtual-screen" },
  pet: { name: "settings-pet" },
  licenses: { name: "settings-licenses" },
};
const FRIENDS_LISTS = ["drafts", "notifications", "bookmarks", "blocked"] as const;

/** The place a page belongs to. */
function placeKeyOfPage(page: PlacePage): "settings" | "friends" | "extensions" | "work" {
  if (page.name === "workFile") return "work";
  return page.name.split("-")[0] as "settings" | "friends" | "extensions";
}

/** The page's path under its place (`computer?id=…`, `post/<id>`), from the place's own. */
function pagePath(page: PlacePage): string {
  const e = encodeURIComponent;
  switch (page.name) {
    case "settings-computer": return `/settings/computer?id=${e(page.id)}`;
    case "settings-license": return `/settings/license?id=${e(page.id)}`;
    case "friends-post": return `/friends/post/${e(page.id)}`;
    case "friends-person": return `/friends/person/${e(page.id)}`;
    case "extensions-service": return `/extensions/${e(page.id)}`;
    case "extensions-tools": return `/extensions/${e(page.service)}/tools${page.account ? `?account=${e(page.account)}` : ""}`;
    case "extensions-agent": return `/extensions/agents/${e(page.scope)}`;
    case "workFile": return `/work/${e(page.id)}`;
    default: {
      if (page.name.startsWith("friends-")) return `/friends/${page.name.slice("friends-".length)}`;
      const segment = Object.keys(SETTINGS_PAGES).find((key) => SETTINGS_PAGES[key].name === page.name);
      return `/settings/${segment}`;
    }
  }
}

/** A page a place's path names below it; null for none (the place's own page). */
function pageOfSegments(first: string, rest: string[], params: URLSearchParams): PlacePage | null {
  const [second = "", third = ""] = rest;
  switch (first) {
    case "settings": {
      if (second === "computer" || second === "license") {
        const id = params.get("id");
        return id ? { name: second === "computer" ? "settings-computer" : "settings-license", id } : null;
      }
      return SETTINGS_PAGES[second] ?? null;
    }
    case "friends":
      if ((second === "post" || second === "person") && third) return { name: second === "post" ? "friends-post" : "friends-person", id: third };
      return (FRIENDS_LISTS as readonly string[]).includes(second) ? { name: `friends-${second}` as PlacePage["name"] } as PlacePage : null;
    case "extensions":
    case "plugins":
      if (!second) return null;
      if (second === "agents") return third ? { name: "extensions-agent", scope: third } : null;
      if (third === "tools") return { name: "extensions-tools", service: second, ...(params.get("account") ? { account: params.get("account")! } : {}) };
      return { name: "extensions-service", id: second };
    case "work":
      return second ? { name: "workFile", id: second } : null;
    default:
      return null;
  }
}

/** The sidebar row a place selects: a Session selects Sessions, a Work file Brain, the computer page Settings. */
export function sidebarKeyOf(place: WebPlace): DesktopPlaceKey {
  if (place.key === "work") return "brain";
  if (place.key === "computer") return "settings";
  if (place.key === "session") return "sessions";
  return place.key;
}

/**
 * Where a place lives: its name (`/calendar`, `/sessions/<id>`), the same as the
 * daemon's app routes (`/screen`, `/resources`, `/friends`). The page carries
 * `<base href="/">`, so a deep path loads its assets from the root. `/plugins` is where the
 * daemon's OAuth callback returns a web sign-in (daemon/connections/oauth.go).
 */
export function placePath(place: WebPlace): string {
  if ("page" in place && place.page) return pagePath(place.page);
  switch (place.key) {
    case "brain": return "/";
    case "session": return `/sessions/${encodeURIComponent(place.workerId)}`;
    default: return `/${place.key}`;
  }
}

/** The place a path names; null for a path that is not one (the app then opens Brain). */
export function placeOfPath(location: string): WebPlace | null {
  let url: URL;
  try {
    url = new URL(location, "http://mewla.invalid");
  } catch {
    return null;
  }
  const segments = url.pathname.split("/").filter(Boolean).map((segment) => {
    try {
      return decodeURIComponent(segment);
    } catch {
      return segment;
    }
  });
  const first = segments[0] ?? "";
  const page = pageOfSegments(first, segments.slice(1), url.searchParams);
  if (page) return { key: placeKeyOfPage(page), page };
  switch (first) {
    case "": return { key: "brain" };
    case "sessions": {
      const workerId = segments[1] ?? "";
      return workerId ? { key: "session", workerId } : { key: "sessions" };
    }
    case "computer": return { key: "computer" };
    case "calendar": return { key: "calendar" };
    case "extensions":
    case "plugins":
      return { key: "extensions" };
    case "screen": return { key: "screen" };
    case "stats": return { key: "stats" };
    case "resources": return { key: "resources" };
    case "friends": return { key: "friends" };
    case "settings": return { key: "settings" };
    default: return null;
  }
}

/** The browser tab's title: the page's own when it has one (a Session's name), else the place's. */
export function documentTitle(place: WebPlace | null, pageTitle?: string | null): string {
  const title = pageTitle?.trim() || (place ? placeTitle(place) : "");
  return title ? `${title} · Mewla` : "Mewla";
}

function placeTitle(place: WebPlace): string {
  switch (place.key) {
    case "work": return "Work";
    case "session": return "Session";
    case "computer": return "Computer";
    default: return placeLabel(place.key);
  }
}

/** The sidebar's width: dragged, saved, and clamped to its range. */
export const SIDEBAR_WIDTH = { initial: 264, min: 216, max: 400 } as const;
/** Where the sidebar's width is kept. */
export const SIDEBAR_WIDTH_KEY = "mewla.desktop.sidebarWidth";

export function clampPanelWidth(value: number, min: number, max: number): number {
  if (!Number.isFinite(value)) return min;
  return Math.round(Math.min(max, Math.max(min, value)));
}

/** A saved width, or the default when nothing (or nothing numeric) was saved. */
export function storedSidebarWidth(raw: string | null | undefined): number {
  const value = raw == null || raw.trim() === "" ? NaN : Number(raw);
  return clampPanelWidth(Number.isFinite(value) ? value : SIDEBAR_WIDTH.initial, SIDEBAR_WIDTH.min, SIDEBAR_WIDTH.max);
}

/** A page's content column (client/DESIGN.md): 760, 1200 (Calendar, Resources, a Session), 820 (a chat), or the whole pane. */
export type PageColumn = "page" | "wide" | "reading" | "full";

export const PAGE_COLUMN_WIDTH: Record<Exclude<PageColumn, "full">, number> = { page: 760, wide: 1200, reading: 820 };

/** The header row's width for a column; null for the whole pane. */
export function pageColumnWidth(column: PageColumn): number | null {
  return column === "full" ? null : PAGE_COLUMN_WIDTH[column];
}
