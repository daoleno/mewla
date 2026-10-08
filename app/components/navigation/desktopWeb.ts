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

export type DesktopNavigation = "switch" | "open";

/**
 * How a sidebar row navigates on desktop web: every click is a new browser
 * history entry, so Back always returns to the page you came from. Brain and
 * Sessions switch in place while their page is showing (the tab switch is
 * itself a history step) and open as a new page otherwise.
 */
export function desktopSidebarNavigation(
  stackRouteNames: readonly string[],
  key: DesktopSidebarKey,
): DesktopNavigation {
  if (desktopRootRouteName(key) !== "(primary)") return "open";
  return stackRouteNames[stackRouteNames.length - 1] === "(primary)" ? "switch" : "open";
}

/** Pages kept mounted besides Brain/Sessions; older ones remount on Back. */
export const DESKTOP_RETAINED_PAGES = 3;

/**
 * Whether a root Stack page stays mounted. Only the newest copy of a page
 * does (a page opened again leaves its older copy as a history entry), and
 * of those only Brain/Sessions and the newest few, so Back keeps recent
 * scroll and state while a long session never keeps every visit alive.
 * Drafts are cached outside the page, so a remount keeps them.
 */
export function desktopScreenRetained(stackRouteNames: readonly string[], index: number): boolean {
  const name = stackRouteNames[index];
  if (name === undefined) return true;
  if (stackRouteNames.indexOf(name, index + 1) !== -1) return false;
  return name === "(primary)" || index >= stackRouteNames.length - DESKTOP_RETAINED_PAGES;
}
