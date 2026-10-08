/** From this width a browser gets the desktop web app (see DESIGN.md). */
export const DESKTOP_WEB_MIN_WIDTH = 1024;

/**
 * The one gate for desktop web: a browser at least 1024 pt wide. Phone web and
 * native, tablets included, keep the phone and tablet model.
 */
export function isDesktopWeb(platform: string, width: number): boolean {
  return platform === "web" && width >= DESKTOP_WEB_MIN_WIDTH;
}

export type DesktopSidebarKey =
  | "brain"
  | "sessions"
  | "calendar"
  | "plugins"
  | "skills"
  | "stats"
  | "resources"
  | "settings";

function firstSegment(pathname: string): string {
  return pathname.replace(/^\/+/, "").split(/[/?#]/)[0] ?? "";
}

/** Where each sidebar row lives. */
export function desktopSidebarPath(key: DesktopSidebarKey): string {
  switch (key) {
    case "brain":
      return "/";
    case "sessions":
      return "/list";
    default:
      return `/${key}`;
  }
}

/** The sidebar row that owns a path: a Session selects Sessions, and so on. */
export function desktopSidebarKey(pathname: string): DesktopSidebarKey | null {
  switch (firstSegment(pathname)) {
    case "":
    case "work":
      return "brain";
    case "list":
    case "terminal":
      return "sessions";
    case "calendar":
      return "calendar";
    case "plugins":
      return "plugins";
    case "skills":
      return "skills";
    case "stats":
      return "stats";
    case "resources":
      return "resources";
    case "settings":
    case "model-profiles":
      return "settings";
    default:
      return null;
  }
}

const PAGE_TITLES: Record<string, string> = {
  "": "Brain",
  list: "Sessions",
  terminal: "Session",
  work: "Work",
  calendar: "Calendar",
  plugins: "Plugins",
  skills: "Skills",
  stats: "Stats",
  resources: "Resources",
  settings: "Settings",
  "model-profiles": "Model Providers",
  browser: "Browser",
  onboarding: "Pair",
};

/** The browser tab title for a path; a page may refine it (a Session's name). */
export function desktopDocumentTitle(pathname: string, pageTitle?: string): string {
  const title = pageTitle?.trim() || PAGE_TITLES[firstSegment(pathname)];
  return title ? `${title} · Mewla` : "Mewla";
}

/** Paths the desktop shell leaves alone: pairing and the screenshot fixtures. */
export function desktopShellExcluded(pathname: string): boolean {
  const segment = firstSegment(pathname);
  return segment === "onboarding" || segment === "screenshot-demo";
}

/**
 * Stack routes that are menu destinations. On desktop web the sidebar is their
 * way out, so they draw no Back; sub-pages keep theirs. `index` is the Plugins
 * catalog in its nested Stack.
 */
export function isDesktopTopLevelRoute(routeName: string): boolean {
  switch (routeName) {
    case "calendar":
    case "index":
    case "skills":
    case "stats":
    case "resources":
    case "settings":
      return true;
    default:
      return false;
  }
}

/**
 * Pages that hold a readable maximum width on desktop web. Brain, Sessions
 * and a Session lay out their own chat width; the terminal grid and pairing
 * stay full width.
 */
export function desktopReadableRoute(routeName: string): boolean {
  switch (routeName) {
    case "(primary)":
    case "plugins":
    case "terminal/[id]":
    case "onboarding":
    case "screenshot-demo":
    case "browser":
      return false;
    default:
      return true;
  }
}

/** The root Stack route that hosts each sidebar row. */
export function desktopRootRouteName(key: DesktopSidebarKey): string {
  return key === "brain" || key === "sessions" ? "(primary)" : key;
}

export type DesktopNavigation = "switch" | "home" | "open";

/**
 * How a sidebar row navigates on desktop web. Every page opens as a new
 * history entry. Brain and Sessions are home: they switch in place when home
 * is showing, and otherwise the stack unwinds to home (the browser moves
 * back to its entry), so the chats are never mounted twice.
 */
export function desktopSidebarNavigation(
  stackRouteNames: readonly string[],
  key: DesktopSidebarKey,
): DesktopNavigation {
  if (desktopRootRouteName(key) !== "(primary)") return "open";
  return stackRouteNames[stackRouteNames.length - 1] === "(primary)" ? "switch" : "home";
}

/** Pages kept mounted above home; older ones remount when you come back. */
export const DESKTOP_RETAINED_PAGES = 3;

/**
 * Whether a root Stack page stays mounted: home always, and the newest few
 * pages above it, so Back keeps their scroll while a long browsing session
 * does not keep every page it visited alive.
 */
export function desktopScreenRetained(index: number, stackLength: number): boolean {
  return index <= 0 || index >= stackLength - DESKTOP_RETAINED_PAGES;
}
