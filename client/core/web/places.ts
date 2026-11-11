/**
 * The desktop layout's places: the sidebar rows, the URL of each place, the
 * browser tab's title, and the sidebar's width. Pure, so the shell and the
 * page host agree without a browser (client/DESIGN.md "Desktop web").
 */

/** From this width a non-phone window gets the desktop layout; below it the tabs stay. */
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

/** A place with a URL: a sidebar row, Brain's Work list, or one Session of the current computer. */
export type WebPlace =
  | { key: Exclude<DesktopPlaceKey, "sessions"> }
  | { key: "sessions" }
  | { key: "work" }
  | { key: "session"; workerId: string };

/** The sidebar row a place selects: a Session selects Sessions, Work selects Brain. */
export function sidebarKeyOf(place: WebPlace): DesktopPlaceKey {
  if (place.key === "work") return "brain";
  if (place.key === "session") return "sessions";
  return place.key;
}

/**
 * Where a place lives: its name (`/calendar`, `/sessions/<id>`), the same as the
 * daemon's app routes (`/screen`, `/resources`, `/friends`). The page carries
 * `<base href="/">`, so a deep path loads its assets from the root. The Expo web
 * app's paths (`/list`, `/plugins`, `/terminal/<id>`) still open their place.
 */
export function placePath(place: WebPlace): string {
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
  switch (first) {
    case "": return { key: "brain" };
    case "work": return { key: "work" };
    case "sessions":
    case "list":
    // The Expo app's Session was `/terminal/<id>?serverId=…`.
    case "terminal": {
      const workerId = segments[1] ?? url.searchParams.get("id") ?? "";
      return workerId ? { key: "session", workerId } : { key: "sessions" };
    }
    case "calendar": return { key: "calendar" };
    case "extensions":
    case "plugins":
      return { key: "extensions" };
    case "screen":
    // The Phone was a row in Screen.
    case "phone":
      return { key: "screen" };
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
    default: return placeLabel(place.key);
  }
}

/** The sidebar's width: dragged, saved, and clamped to its range. */
export const SIDEBAR_WIDTH = { initial: 264, min: 216, max: 400 } as const;
/** The Expo web app's key: its localStorage entries are copied over once, so the width carries over. */
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
