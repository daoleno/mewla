/**
 * The app's places: the URL of each place, the browser tab's title, the wide layout's gate and
 * its left pane's width. Pure, so the shell and the page host agree without a browser
 * (client/DESIGN.md "The wide layout", soft-final SPEC §14).
 */

/** From this usable width (points) the app has two panes, on every device: a browser, a tablet, an open foldable. */
export const WIDE_MIN_WIDTH = 1024;
/**
 * A window on its side has two panes from this usable width already: an open foldable held like a book (the iPhone
 * Duo's inner screen on its side, 867 pt beside its 84 pt side bar; the Android SDK's 7.6" Fold-in, 841 x 674 dp,
 * less a side cutout: 840, Material's "expanded", would miss it), so the tray
 * stands beside a page at least 440 wide. Upright or short it stays one stack (Apple's split view tiles only
 * landscape; a phone held sideways is under WIDE_MIN_HEIGHT).
 */
export const WIDE_LANDSCAPE_MIN_WIDTH = 760;
/** Below this height a window on its side keeps one stack: two panes over a short phone screen read as neither. */
export const WIDE_MIN_HEIGHT = 480;

/**
 * The one gate for the wide layout (SPEC §14.6), whatever the device. `width` is the usable width: the window less
 * what the system draws at its sides (the iPhone Duo's side bar, a phone's cutout), never counted as room.
 */
export function isWide(window: { width: number; height: number }): boolean {
  if (window.width >= WIDE_MIN_WIDTH) return true;
  return window.width >= WIDE_LANDSCAPE_MIN_WIDTH && window.width > window.height && window.height >= WIDE_MIN_HEIGHT;
}

/**
 * The gate as the window changes: a window that was wide stays wide while only its height shrinks at the same width
 * and it stays wider than tall. Android 14 and older resize the app's window over the software keyboard (Mewla's
 * `adjustResize`; 15 and later draw edge to edge and report the keyboard apart), so a Fold on its side, 674 high,
 * would fall under WIDE_MIN_HEIGHT on each tap into a field and lose its pane until the keyboard went. A phone turned
 * on its side starts short, so it never becomes wide this way; a turn, a fold or a resize changes the width and
 * decides afresh.
 */
export function nextWide(previous: { wide: boolean; width: number } | null, window: { width: number; height: number }): boolean {
  if (isWide(window)) return true;
  return Boolean(previous?.wide) && previous!.width === window.width && window.width > window.height;
}

/** A place you can go to by name: a `G` key, the palette's Go to, a URL. */
export type PlaceKey =
  | "brain"
  | "sessions"
  | "calendar"
  | "computer"
  | "extensions"
  | "screen"
  | "stats"
  | "resources"
  | "friends"
  | "settings";

const LABELS: Record<PlaceKey, string> = {
  brain: "Brain",
  sessions: "All Sessions",
  calendar: "Calendar",
  computer: "Computer",
  extensions: "Extensions",
  screen: "Screen",
  stats: "Stats",
  resources: "Resources",
  friends: "Friends",
  settings: "Settings",
};

export function placeLabel(key: PlaceKey): string {
  return LABELS[key] ?? key;
}

/**
 * A page under a place that has a URL of its own, as the Expo app's routes did: a Settings page
 * (`/settings/computer?id=…`), a Friends post or person, a Plugins service, its tools or a tool's
 * extensions, a Work file (`/work/<id>`), a Work's detail (`/work-detail/<id>`, SPEC §14.5). The values are the shell's
 * own routes, but for the Work detail, which carries `workId` there.
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
  | { name: "workFile"; id: string }
  | { name: "workDetail"; id: string };

/** A place with a URL: a named place, a Work file, or one Session of the current computer; `page`, a page under it. */
export type WebPlace =
  | { key: Exclude<PlaceKey, "sessions" | "computer">; page?: PlacePage }
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
  if (page.name === "workFile" || page.name === "workDetail") return "work";
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
    case "workDetail": return `/work-detail/${e(page.id)}`;
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
    case "work-detail":
      return second ? { name: "workDetail", id: second } : null;
    case "brain":
      // The app's own link for a Work (mewla://brain/work/<id>), read as a path.
      return second === "work" && third ? { name: "workDetail", id: third } : null;
    default:
      return null;
  }
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
    default: return placeLabel(place.key);
  }
}

/** The left pane's width on wide (SPEC §14.1): dragged within 280–420, 320 by default, ← and → step 16. */
export const PANE_WIDTH = { initial: 320, min: 280, max: 420, step: 16 } as const;
/** Where the left pane's width is kept on this device. */
export const PANE_WIDTH_KEY = "mewla.wide.paneWidth";

export function clampPaneWidth(value: number): number {
  if (!Number.isFinite(value)) return PANE_WIDTH.initial;
  return Math.round(Math.min(PANE_WIDTH.max, Math.max(PANE_WIDTH.min, value)));
}

/** A saved width, or the default when nothing (or nothing numeric) was saved. */
export function storedPaneWidth(raw: string | null | undefined): number {
  const value = raw == null || raw.trim() === "" ? NaN : Number(raw);
  return clampPaneWidth(Number.isFinite(value) ? value : PANE_WIDTH.initial);
}

/** ← (-1) or → (+1) on the focused handle. */
export function stepPaneWidth(width: number, direction: -1 | 1): number {
  return clampPaneWidth(width + direction * PANE_WIDTH.step);
}

/** From this right-pane width a list page shows the item it opens beside it (Git review › diff, Files › file). */
export const LIST_DETAIL_MIN_WIDTH = 880;

/** The list and its item side by side: on wide, with a right pane of at least 880 pt; otherwise the item pushes. */
export function showsListDetail(input: { wide: boolean; paneWidth: number }): boolean {
  return input.wide && input.paneWidth >= LIST_DETAIL_MIN_WIDTH;
}

/** A page's content column (client/DESIGN.md): 680 for list pages, 760 for a thread and its composer, or the whole pane. */
export type PageColumn = "page" | "reading" | "full";

export const PAGE_COLUMN_WIDTH: Record<Exclude<PageColumn, "full">, number> = { page: 680, reading: 760 };

/**
 * The width a page's own title view gets in a native header (iOS, Android). NSH lays a header item out at
 * its content's width, and the bar takes that size as it is: a long title made the row wider than the
 * screen, and the Session's switch and ⋯ went past its edge, the title cut there with no "…" (owner
 * 11-13). Given the bar's width instead, the title ends in "…" before the controls. Android: the toolbar
 * starts its title 16 after the edge, and the row stops 8 before the other (the mock's nav padding).
 * iOS: 16 each side, after the system's Back when it shows (not proven on a device).
 */
export function nativeHeaderTitleWidth(options: { stackWidth: number; os: string; systemBack: boolean }): number {
  const sides = options.os === "android" ? 16 + 8 : 16 + 16 + (options.systemBack ? 56 : 0);
  return Math.max(0, Math.floor(options.stackWidth - sides));
}

/** The header row's width for a column; null for the whole pane. */
export function pageColumnWidth(column: PageColumn): number | null {
  return column === "full" ? null : PAGE_COLUMN_WIDTH[column];
}
