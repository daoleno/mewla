/** From this width a browser gets the desktop web app (see DESIGN.md). */
export const DESKTOP_WEB_MIN_WIDTH = 1024;

/**
 * The one gate for desktop web: a browser at least 1024 pt wide. Phone web and
 * native, tablets included, keep the phone and tablet model.
 */
export function isDesktopWeb(platform: string, width: number): boolean {
  return platform === "web" && width >= DESKTOP_WEB_MIN_WIDTH;
}

/** Any touch input at all, primary or not (touch laptops included). */
export const TOUCH_POINTER_QUERY = "(any-pointer: coarse)";

/**
 * Web with no touch pointer, so typing comes from a physical keyboard and
 * phone keyboard aids (the terminal's key row) stay out. `touch` is the
 * TOUCH_POINTER_QUERY match, or null where media queries are unavailable.
 */
export function isKeyboardOnlyWeb(platform: string, touch: boolean | null): boolean {
  return platform === "web" && touch === false;
}

export type DesktopSidebarKey =
  | "brain"
  | "sessions"
  | "calendar"
  | "plugins"
  | "screen"
  | "stats"
  | "resources"
  | "friends"
  | "phone"
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
    case "screen":
      return "screen";
    case "stats":
      return "stats";
    case "resources":
      return "resources";
    case "friends":
      return "friends";
    case "phone":
      return "phone";
    case "settings":
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
  plugins: "Extensions",
  screen: "Screen",
  stats: "Stats",
  resources: "Resources",
  friends: "Friends",
  phone: "Phone use",
  settings: "Settings",
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
 * way out, so they draw no Back; sub-pages keep theirs. `index` is the
 * Extensions page in its nested Stack.
 */
export function isDesktopTopLevelRoute(routeName: string): boolean {
  switch (routeName) {
    case "calendar":
    case "index":
    case "screen":
    case "stats":
    case "resources":
    case "friends":
    case "phone":
    case "settings":
      return true;
    default:
      return false;
  }
}

/** A desktop page's content column (see ContentWidth). */
export type DesktopPageColumn = "page" | "wide";

/**
 * The column a page's header row shares with its content, so the title sits
 * where the content starts. Null: the browser and Screen run full width.
 */
export function desktopPageColumn(routeName: string): DesktopPageColumn | null {
  switch (routeName) {
    case "browser":
    case "screen":
    case "onboarding":
    case "screenshot-demo":
      return null;
    case "calendar":
    case "resources":
      return "wide";
    default:
      return "page";
  }
}

/**
 * Routes that center their own content column inside a full-width scroll
 * area (so the scrollbar stays at the window edge) or lay out their own
 * width: the menu destinations, Brain, Sessions, a Session, the browser.
 */
const SELF_FRAMED_ROUTES: ReadonlySet<string> = new Set([
  "(primary)",
  "plugins",
  "index",
  "terminal/[id]",
  "onboarding",
  "screenshot-demo",
  "browser",
  "screen",
  "calendar",
  "resources",
  "settings",
  "stats",
  "settings/computer",
  "settings/licenses",
  "settings/license",
]);

/**
 * Whether the Stack holds this route's screen in the centered page column
 * (`ContentWidth.page`): sub-pages and lists that have no column of their own.
 */
export function desktopFramedRoute(routeName: string): boolean {
  return !SELF_FRAMED_ROUTES.has(routeName);
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
