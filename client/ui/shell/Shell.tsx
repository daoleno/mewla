import { computed, effect, host, platform, signal, Storage, Text, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { Button, createNavStack, NavStack, ResizablePanel, ResizablePanelGroup, type NavStackState } from "nsh/base";
import { PageStack } from "./NavPage";
import { isAvailable as mobile, TabBar } from "nsh/mobile";
import { reuseRoutes, type PoppedPage } from "../../core/web/history";
import { clampPanelWidth, isDesktopLayout, SIDEBAR_WIDTH, SIDEBAR_WIDTH_KEY, sidebarKeyOf, storedSidebarWidth, type DesktopPlaceKey, type WebPlace } from "../../core/web/places";
import { DesktopOverlays } from "./desktop/Overlays";
import { Sidebar } from "./desktop/Sidebar";
import { syncWebLocation, type PageSnapshot } from "./webLocation";
import { Onboarding, PairPage } from "../screens/Onboarding";
import { More } from "../screens/More";
import { MorePageView } from "../more/Pages";
import { FilesPageView } from "../more/files/Pages";
import { GitPageView } from "../more/git/GitPages";
import type { MorePage, PageNav } from "../more/routes";
import { Brain } from "../screens/Brain";
import { calendarNav, calendarRoute, showCalendarItem } from "../calendar/routes";
import { WorkFilePage } from "../work/WorkFilePage";
import { workRoute, type BrainWorkRoute } from "../work/routes";
import { Session } from "../screens/Session";
import { Sessions } from "../screens/Sessions";
import { NewSession, newSessionOpen } from "../screens/NewSession";
import { ServicesSheet } from "../sessions/ServicesSheet";
import { EnrollmentApproval } from "../enrollment/Approval";
import { BrowserJoin } from "../enrollment/BrowserJoin";
import { current, onTransport, workers } from "../session";
import { routeOwner, screenRoute } from "../../core/screen/float";
import { ScreenFloatHost } from "../screenfloat/ScreenFloatHost";
import { workFiles } from "../work/state";
import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { MoreRoute, SessionsRoute } from "./routes";
import { needsYou } from "./attention";
import { settingsLinkRoutes } from "../../core/settings/settingsLink";
import { setOpenExtensions } from "../more/extensions/plugins";
import { tabTitle } from "../../core/sessions/attention";

const tabs = [
  { key: "brain", title: "Brain", ios: "sparkles", android: "tab_brain" },
  { key: "sessions", title: "Sessions", ios: "list.bullet", android: "tab_sessions" },
  { key: "calendar", title: "Calendar", ios: "calendar", android: "tab_calendar" },
  { key: "more", title: "More", ios: "ellipsis", android: "tab_more" },
];

export const selectedTab = signal(0);
/** What lies below the sections (the TabBar, the system bar): the keyboard covers it before it reaches a page. */
export const belowSections = signal(0);
/**
 * The desktop layout (a browser or a desktop window at least 1024 pt wide): a
 * sidebar beside every page instead of the tabs (client/DESIGN.md "Desktop web").
 */
export const desktop = computed(() => isDesktopLayout({ mobile, width: windowSize().width }));

/** The section a `mewla://` link opens: `mewla://brain`, `…/sessions`, `…/calendar`, `…/more`. */
export function sectionOfLink(url: string): number | null {
  const match = /^mewla:\/\/([a-z]+)/i.exec(url.trim());
  const index = match ? ["brain", "sessions", "calendar", "more"].indexOf(match[1].toLowerCase()) : -1;
  return index < 0 ? null : index;
}
const moreNav = createNavStack<MoreRoute>([{ name: "more" }]);
export const brainNav = createNavStack<{ name: "brain" } | BrainWorkRoute>([{ name: "brain" }]);
const sessionsNav = createNavStack<SessionsRoute>([{ name: "sessions" }]);
// A Session page belongs to the computer it was opened on: a switch goes back to the list.
onTransport(() => () => sessionsNav.popToRoot());

/** Opens a Session of the current computer on the Sessions tab (New session, Work, Calendar, Brain cards). */
export function openSession(workerId: string, hint?: TerminalRouteSessionHint) {
  selectedTab.set(1);
  sessionsNav.setRoutes([sessionsNav.routes()[0] ?? { name: "sessions" }, { name: "session", workerId, hint }]);
}

/**
 * More's stack. On a phone it starts at the More list; on the desktop its pages
 * are the sidebar's places, so a place is the stack's root and has no Back.
 */
function moreRoutes(routes: readonly MoreRoute[]): MoreRoute[] {
  const pages = routes[0]?.name === "more" ? routes.slice(1) : [...routes];
  if (!untracked(desktop)) return [routes[0]?.name === "more" ? routes[0] : { name: "more" }, ...pages];
  return pages.length ? pages : [{ name: "settings" }];
}

/** Opens a More page (and the ones under it) on the More tab, or as a desktop place. */
export function openMore(...routes: MoreRoute[]) {
  selectedTab.set(3);
  moreNav.setRoutes(moreRoutes(routes));
}

/** `mewla://settings…` (not a pairing link): the Settings page it names, under More. True when it was one. */
export function openSettingsLink(url: string): boolean {
  const routes = settingsLinkRoutes(url);
  if (!routes) return false;
  openMore(...routes);
  return true;
}

// A Plugins sign-in that came back (a browser tab, a phone's link) opens Extensions, where its card finishes it.
setOpenExtensions(() => openPlace({ key: "extensions" }));

const PLACE_ROUTES: Record<Exclude<DesktopPlaceKey, "brain" | "sessions" | "calendar">, MoreRoute> = {
  extensions: { name: "extensions" },
  screen: { name: "screen" },
  stats: { name: "stats" },
  resources: { name: "resources" },
  friends: { name: "friends" },
  settings: { name: "settings" },
};

/** Goes to a place at its root: a sidebar row, the palette, `G` then a letter, a URL. */
export function openPlace(place: WebPlace) {
  switch (place.key) {
    case "brain":
      selectedTab.set(0);
      return brainNav.popToRoot();
    case "work":
      selectedTab.set(0);
      return brainNav.setRoutes([brainNav.routes()[0], { name: "work" }]);
    case "sessions":
      selectedTab.set(1);
      return sessionsNav.popToRoot();
    case "session":
      return openSession(place.workerId);
    case "calendar":
      selectedTab.set(2);
      return calendarNav.popToRoot();
    default:
      return openMore(PLACE_ROUTES[place.key]);
  }
}

/** The place a More stack shows: the first page above the More list decides. */
function placeOfMore(routes: readonly MoreRoute[]): WebPlace {
  const name = routes.find((route) => route.name !== "more")?.name ?? "settings";
  if (name.startsWith("extensions")) return { key: "extensions" };
  if (name.startsWith("stats")) return { key: "stats" };
  if (name.startsWith("friends")) return { key: "friends" };
  if (name === "screen" || name === "resources") return { key: name };
  return { key: "settings" };
}

/** Where the person is: what the URL, the tab title and the sidebar's selected row follow. */
export const currentPlace = computed<WebPlace>(() => {
  switch (selectedTab()) {
    case 0:
      return brainNav.routes().length > 1 ? { key: "work" } : { key: "brain" };
    case 1: {
      const session = [...sessionsNav.routes()].reverse().find((route) => route.name === "session");
      return session?.name === "session" ? { key: "session", workerId: session.workerId } : { key: "sessions" };
    }
    case 2:
      return { key: "calendar" };
    default:
      return placeOfMore(moreNav.routes());
  }
});
export const selectedPlace = computed(() => sidebarKeyOf(currentPlace()));

const stacks = [brainNav, sessionsNav, calendarNav, moreNav] as const;

/** A history entry: the section and its stack. */
function snapshot(): PageSnapshot {
  const tab = selectedTab();
  return { tab, routes: stacks[tab]?.routes() ?? [] };
}

/** Reopens what a history entry showed, or a place a URL names. */
function restore(page: PoppedPage<PageSnapshot>) {
  if (page.kind === "place") return openPlace(page.place);
  const { tab, routes } = page.snapshot;
  const stack = stacks[tab] as (typeof stacks)[number] | undefined;
  if (!stack || !routes.length) return openPlace({ key: "brain" });
  selectedTab.set(tab);
  const next = reuseRoutes(stack.routes() as unknown[], routes);
  (stack.setRoutes as (routes: unknown[]) => void)(tab === 3 ? moreRoutes(next as MoreRoute[]) : next);
}

/** The sidebar's width: dragged, saved (the Expo web app's key), restored at the next start. */
export const sidebarWidth = signal<number>(SIDEBAR_WIDTH.initial);
function saveSidebarWidth(size: number) {
  const width = clampPanelWidth(size, SIDEBAR_WIDTH.min, SIDEBAR_WIDTH.max);
  sidebarWidth.set(width);
  void Storage.setItem(SIDEBAR_WIDTH_KEY, String(width)).catch(() => {});
}

/** Phone use's page, under More › Settings: `mewla://phone` (its notifications) and its floating control open it. */
export function openPhonePage() {
  selectedTab.set(3);
  moreNav.setRoutes([{ name: "more" }, { name: "settings" }, { name: "settings-phone" }]);
}

/** Screen, where an agent's display and the Mac's screen have their full view: the floating screen's Open. */
export function openScreenPage(worker: string | null) {
  selectedTab.set(3);
  moreNav.setRoutes([{ name: "more" }, screenRoute(worker)]);
}

/** The page the floating screen floats over: whose Session or Work file it is, and whether it is Screen. */
const floatPage = () => {
  const tab = selectedTab();
  const top = stacks[tab]?.routes().at(-1) as { name: string; workerId?: string; id?: string } | undefined;
  return {
    owner: routeOwner(top, (id) => workFiles().find((item) => item.id === id)?.frontmatter.worker_session),
    screenPage: tab === 3 && top?.name === "screen",
  };
};

/**
 * How a Session's Git review and file previews push their next page: theirs stay on the
 * Sessions stack, anything else opens under More.
 */
const sessionsPageNav: PageNav = {
  push(route: MorePage) {
    if (route.name.startsWith("git") || route.name.startsWith("files-")) sessionsNav.push(route as SessionsRoute);
    else openMore(route);
  },
  pop: () => sessionsNav.pop(),
};

function sessionsRoute(route: SessionsRoute, index: () => number) {
  switch (route.name) {
    case "sessions":
      return <Sessions nav={sessionsNav} onNew={() => newSessionOpen.set(true)} onPair={() => openMore({ name: "settings" }, { name: "pair" })} />;
    case "session":
      return <Session nav={sessionsNav} workerId={route.workerId} hint={route.hint}
        active={() => selectedTab() === 1 && index() === sessionsNav.routes().length - 1} />;
    case "workFile":
      return <WorkFilePage id={route.id} onClose={() => sessionsNav.pop()} openCalendarItem={(id, runId) => {
        showCalendarItem(id, runId);
        selectedTab.set(2);
      }} />;
    case "files-preview":
    case "files-workspace":
      return <FilesPageView route={route} nav={sessionsPageNav} />;
    default:
      return <GitPageView route={route} nav={sessionsPageNav} />;
  }
}

function moreRoute(route: MoreRoute) {
  switch (route.name) {
    case "more": return <More nav={moreNav} />;
    case "pair": return <PairPage onPaired={() => moreNav.pop()} />;
    default: return <MorePageView route={route} nav={moreNav} onPair={() => moreNav.push({ name: "pair" })} />;
  }
}

/**
 * A stack on the desktops and the web, where a page draws its own header: its pages learn
 * whether there is a Back (the stack holds more than its first page; only the top one shows)
 * and what Back does. Around the stack, not each page: a NavStack's page must be its NavPage.
 */
function Paged<R>(props: { nav: NavStackState<R>; children: JSX.Element }) {
  if (mobile) return props.children;
  return <PageStack.Provider value={{ canGoBack: () => props.nav.routes().length > 1, back: () => props.nav.pop() }}>{props.children}</PageStack.Provider>;
}

/** One native stack per tab; every stack stays mounted so a tab keeps its place. */
function Section(props: { index: number }) {
  const shown = () => selectedTab() === props.index;
  return (
    <View class="absolute inset-0" style={{ display: shown() ? "flex" : "none" }}>
      {props.index === 0 ? <Paged nav={brainNav}><NavStack state={brainNav} style={{ flex: 1 }}>{(route) => (route.name === "brain" ? <Brain /> : workRoute(route, brainNav))}</NavStack></Paged>
        : props.index === 1 ? <Paged nav={sessionsNav}><NavStack state={sessionsNav} style={{ flex: 1 }}>{sessionsRoute}</NavStack></Paged>
        : props.index === 2 ? <Paged nav={calendarNav}><NavStack state={calendarNav} style={{ flex: 1 }}>{calendarRoute}</NavStack></Paged>
        : <Paged nav={moreNav}><NavStack state={moreNav} style={{ flex: 1 }}>{moreRoute}</NavStack></Paged>}
    </View>
  );
}

/**
 * The Session's own name for the tab title, as the list knows it. A computed: the list changes
 * with every status line, and only a new name may rewrite the history entry.
 */
const pageTitle = computed((): string | null => {
  const place = currentPlace();
  if (place.key !== "session") return null;
  return workers().find((worker) => worker.id === place.workerId)?.name || null;
});

/**
 * A browser or a desktop window. From 1024 pt the sidebar sits beside every
 * page (resizable, its width saved); below it a row of the tabs. One tree for
 * both, so crossing the width never remakes the sections.
 */
function WideShell() {
  void Storage.getItem(SIDEBAR_WIDTH_KEY).then((raw) => sidebarWidth.set(storedSidebarWidth(raw))).catch(() => {});
  // More's pages are places on the desktop and sit under the More list below it.
  effect(() => {
    desktop();
    const routes = untracked(moreNav.routes);
    const next = moreRoutes(routes);
    if (next.length !== routes.length || next.some((route, index) => route !== routes[index])) moreNav.setRoutes(next);
  });
  syncWebLocation({ place: currentPlace, snapshot, pageTitle, serverId: () => current()?.id ?? null, restore });
  return (
    <View class="flex-1 bg-background">
      <ResizablePanelGroup class="flex-1" handleClass="bg-border group-hover:bg-ring group-active:bg-ring" handleAccessibilityLabel="Resize sidebar">
        <ResizablePanel visible={desktop()} fixed size={sidebarWidth()} minSize={SIDEBAR_WIDTH.min} maxSize={SIDEBAR_WIDTH.max} onResize={({ size }) => saveSidebarWidth(size)}>
          <Sidebar />
        </ResizablePanel>
        <ResizablePanel minSize={320}>
          {desktop() ? null : (
            <View testID="tab-row" class="flex-row gap-1 border-b border-border px-2">
              {tabs.map((t, i) => (
                <Button testID={`tab-${t.title}`} onPress={() => selectedTab.set(i)} class="px-3 py-2.5">
                  <Text class={selectedTab() === i ? "text-base font-semibold" : "text-base text-muted-foreground"}>{t.title}</Text>
                </Button>
              ))}
            </View>
          )}
          <View class="flex-1">{tabs.map((_, i) => <Section index={i} />)}</View>
        </ResizablePanel>
      </ResizablePanelGroup>
      <ScreenFloatHost page={floatPage} onOpenPhone={openPhonePage} onOpenScreen={openScreenPage} />
      <NewSession onCreated={(workerId, fromSession, hint) => (fromSession ? sessionsNav.replace({ name: "session", workerId, hint }) : openSession(workerId, hint))} />
      <ServicesSheet onOpenSession={(workerId) => openSession(workerId)} />
      <EnrollmentApproval />
      <DesktopOverlays />
    </View>
  );
}

export function Shell() {
  if (!mobile) return <WideShell />;
  return (
    <View class="flex-1 bg-background">
      <View class="flex-1" onLayout={({ bounds }) => belowSections.set(Math.max(0, windowSize().height - bounds.origin.y - bounds.size.height))}>{tabs.map((_, i) => <Section index={i} />)}</View>
      <ScreenFloatHost page={floatPage} onOpenPhone={openPhonePage} onOpenScreen={openScreenPage} bottomInset={belowSections} />
      <NewSession onCreated={(workerId, fromSession, hint) => (fromSession ? sessionsNav.replace({ name: "session", workerId, hint }) : openSession(workerId, hint))} />
        <ServicesSheet onOpenSession={(workerId) => openSession(workerId)} />
        <EnrollmentApproval />
      <TabBar testID="tabs" items={tabs.map((t, i) => ({ title: tabTitle(t.title, needsYou(t.key), selectedTab() === i), icon: platform.select({ ios: t.ios, android: t.android, default: t.android }) }))}
        selectedIndex={selectedTab()} onChange={({ index }) => selectedTab.set(index)} />
    </View>
  );
}

/**
 * Before any computer: pair one. A browser served by a computer first asks that computer to let
 * it in (BrowserJoin); "Use a pairing link" leads to the pairing page, as on a phone.
 */
export function FirstRun() {
  const web = platform.os === "web" && host.available();
  // A phone: the guided onboarding, which also shows the connected step after any pairing.
  const settings = () => {
    selectedTab.set(3);
    moreNav.setRoutes([{ name: "more" }, { name: "settings" }]);
  };
  if (!web) return <Onboarding onContinue={() => selectedTab.set(0)} onSettings={settings} />;
  const nav = createNavStack<{ name: "join" } | { name: "pair" }>([{ name: "join" }]);
  return (
    <Paged nav={nav}>
      <NavStack state={nav} style={{ flex: 1 }}>
        {(route) => (route.name === "join" ? <BrowserJoin onPairingLink={() => nav.push({ name: "pair" })} /> : <PairPage />)}
      </NavStack>
    </Paged>
  );
}
