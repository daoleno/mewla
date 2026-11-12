import { computed, effect, host, platform, signal, Storage, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { createNavStack, NavStack, ResizablePanel, ResizablePanelGroup, type NavStackState } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { PageStack } from "./NavPage";
import { reuseRoutes, type PoppedPage } from "../../core/web/history";
import { clampPanelWidth, isDesktopLayout, SIDEBAR_WIDTH, SIDEBAR_WIDTH_KEY, sidebarKeyOf, storedSidebarWidth, type WebPlace } from "../../core/web/places";
import { placeOfRoutes, routesOfPlace } from "./routePlaces";
import { DesktopOverlays } from "./desktop/Overlays";
import { Sidebar } from "./desktop/Sidebar";
import { syncWebLocation, type PageSnapshot } from "./webLocation";
import { Onboarding, PairPage } from "../screens/Onboarding";
import { MorePageView } from "../more/Pages";
import { Home, openBrainHost } from "../home/Home";
import { ComputerPage } from "../computer/ComputerPage";
import { calendarRoute } from "../calendar/routes";
import { WorkDetail } from "../work/WorkPage";
import { workLinks } from "../work/links";
import { Session } from "../screens/Session";
import { AllSessions } from "../sessions/AllSessions";
import { NewSession } from "../screens/NewSession";
import { ServicesSheet } from "../sessions/ServicesSheet";
import { EnrollmentApproval } from "../enrollment/Approval";
import { CalendarAlertCard } from "../calendar/AlertCard";
import { BrowserJoin } from "../enrollment/BrowserJoin";
import { current, onTransport, workers } from "../session";
import { routeOwner, screenRoute } from "../../core/screen/float";
import { ScreenFloatHost } from "../screenfloat/ScreenFloatHost";
import { workFiles } from "../work/state";
import { settingsLinkRoutes } from "../../core/settings/settingsLink";
import { setOpenExtensions } from "../more/extensions/plugins";
import { sessionRowModel } from "../../core/sessions/listRow";
import { workLinkOf } from "../sessions/workLinks";
import { goHome, isTop, nav, openPage, openSession, pageNav } from "./nav";
import type { Route } from "./routes";

/** What lies below the stack (the system bar): the keyboard covers it before it reaches a page. */
export const belowSections = signal(0);
/**
 * The desktop layout (a browser or a desktop window at least 1024 pt wide): the sidebar beside
 * the stack (client/DESIGN.md "Desktop web"). Below it, and on phones, the stack alone.
 */
export const desktop = computed(() => isDesktopLayout({ mobile, width: windowSize().width }));

// A computer switch: sheets close first (they listen to the switch themselves), then the stack
// returns to the new computer's home (SPEC §3.8).
onTransport(() => () => goHome());

/** `mewla://settings…` (not a pairing link): the Settings page it names, under the computer page. True when it was one. */
export function openSettingsLink(url: string): boolean {
  const routes = settingsLinkRoutes(url);
  if (!routes) return false;
  // `addServer`: Pair a computer, which lives on the computer page.
  if (routes.at(-1)?.name === "pair") openPage({ name: "computer" }, { name: "pair" });
  else openPage({ name: "computer" }, ...routes);
  return true;
}

// A Plugins sign-in that came back (a browser tab, a phone's link) opens Extensions, where its card finishes it.
setOpenExtensions(() => openPage({ name: "computer" }, { name: "extensions" }));

/** Phone use's page: `mewla://phone` (its notifications) and its floating control open it. */
export function openPhonePage() {
  openPage({ name: "computer" }, { name: "settings" }, { name: "settings-phone" });
}

/** Screen, where an agent's display and the Mac's screen have their full view: the floating screen's Open. */
export function openScreenPage(worker: string | null) {
  openPage({ name: "computer" }, screenRoute(worker));
}

/** Goes to a place a URL, the sidebar, the palette or `G` then a letter names. */
export function openPlace(place: WebPlace) {
  nav.setRoutes([{ name: "home" }, ...routesOfPlace(place)]);
}

/** Where the person is: what the URL, the tab title and the sidebar's selected row follow. */
export const currentPlace = computed<WebPlace>(() => placeOfRoutes(nav.routes()));
export const selectedPlace = computed(() => sidebarKeyOf(currentPlace()));

/** A history entry: the stack. */
function snapshot(): PageSnapshot {
  return { routes: nav.routes() };
}

/** Reopens what a history entry showed, or a place a URL names. */
function restore(page: PoppedPage<PageSnapshot>) {
  if (page.kind === "place") return openPlace(page.place);
  const routes = page.snapshot.routes as Route[];
  if (!routes.length || routes[0]?.name !== "home") return goHome();
  nav.setRoutes(reuseRoutes(untracked(nav.routes) as unknown[], routes) as Route[]);
}

/** The sidebar's width: dragged, saved, restored at the next start. */
export const sidebarWidth = signal<number>(SIDEBAR_WIDTH.initial);
function saveSidebarWidth(size: number) {
  const width = clampPanelWidth(size, SIDEBAR_WIDTH.min, SIDEBAR_WIDTH.max);
  sidebarWidth.set(width);
  void Storage.setItem(SIDEBAR_WIDTH_KEY, String(width)).catch(() => {});
}

/** The page the floating screen floats over: whose Session or Work file it is, and whether it is Screen or the Phone page. */
const floatPage = () => {
  const top = nav.routes().at(-1) as { name: string; workerId?: string; id?: string } | undefined;
  return {
    owner: routeOwner(top, (id) => workFiles().find((item) => item.id === id)?.frontmatter.worker_session),
    screenPage: top?.name === "screen",
    phonePage: top?.name === "settings-phone",
  };
};

function page(route: Route, index: () => number): JSX.Element {
  switch (route.name) {
    case "home": return <Home />;
    case "session":
      return <Session nav={nav} workerId={route.workerId} hint={route.hint} brain={route.brain} active={() => isTop(index())} />;
    case "allSessions": return <AllSessions nav={nav} />;
    case "computer": return <ComputerPage nav={pageNav} onSwitchExecutor={openBrainHost} />;
    case "pair": return <PairPage onScreen={() => isTop(index())} onPaired={() => goHome()} />;
    case "workDetail": return <WorkDetail workId={route.workId} nav={nav} links={workLinks} />;
    case "calendar":
    case "item":
    case "run":
    case "workFile":
      return calendarRoute(route, nav);
    default:
      return <MorePageView route={route} nav={pageNav} onPair={() => nav.push({ name: "pair" })} />;
  }
}

/**
 * On the desktops and the web a page draws its own header: it learns whether there is a Back
 * (the stack holds more than home) and what Back does. Around the stack, not each page: a
 * NavStack's page must be its NavPage.
 */
function Paged<R>(props: { nav: NavStackState<R>; children: JSX.Element }) {
  if (mobile) return props.children;
  return <PageStack.Provider value={{ canGoBack: () => props.nav.routes().length > 1, back: () => props.nav.pop() }}>{props.children}</PageStack.Provider>;
}

function Stack() {
  return (
    <Paged nav={nav}>
      <NavStack state={nav} style={{ flex: 1 }}>{page}</NavStack>
    </Paged>
  );
}

/**
 * The Session's title for the browser tab, as its row and its page name it. A computed: the list
 * changes with every status line, and only a new title may rewrite the history entry.
 */
const pageTitle = computed((): string | null => {
  const place = currentPlace();
  if (place.key !== "session") return null;
  const worker = workers().find((w) => w.id === place.workerId);
  if (!worker) return null;
  const link = workLinkOf(worker);
  return sessionRowModel(worker, { workTitle: link?.workTitle, calendarLabel: link?.calendarLabel }).title || null;
});

/** What every layout lays over the stack. */
function Overlays(props: { bottomInset?: () => number }) {
  return (
    <>
      <ScreenFloatHost page={floatPage} onOpenPhone={openPhonePage} onOpenScreen={openScreenPage} bottomInset={props.bottomInset} />
      <NewSession onCreated={(workerId, fromSession, hint) => (fromSession ? nav.replace({ name: "session", workerId, hint }) : openSession(workerId, hint))} />
      <ServicesSheet onOpenSession={(workerId) => openSession(workerId)} />
      <EnrollmentApproval />
      <CalendarAlertCard />
    </>
  );
}

/**
 * A browser or a desktop window: from 1024 pt the sidebar sits beside the stack (resizable, its
 * width saved); below it the stack alone, as on a phone.
 */
function WideShell() {
  void Storage.getItem(SIDEBAR_WIDTH_KEY).then((raw) => sidebarWidth.set(storedSidebarWidth(raw))).catch(() => {});
  syncWebLocation({ place: currentPlace, snapshot, pageTitle, serverId: () => current()?.id ?? null, restore });
  return (
    <View class="flex-1 bg-background">
      <ResizablePanelGroup class="flex-1" handleClass="bg-border group-hover:bg-ring group-active:bg-ring" handleAccessibilityLabel="Resize sidebar">
        <ResizablePanel visible={desktop()} fixed size={sidebarWidth()} minSize={SIDEBAR_WIDTH.min} maxSize={SIDEBAR_WIDTH.max} onResize={({ size }) => saveSidebarWidth(size)}>
          <Sidebar />
        </ResizablePanel>
        <ResizablePanel minSize={320}>
          <View class="flex-1"><Stack /></View>
        </ResizablePanel>
      </ResizablePanelGroup>
      <Overlays />
      <DesktopOverlays />
    </View>
  );
}

export function Shell() {
  if (!mobile) return <WideShell />;
  return (
    <View class="flex-1 bg-background">
      <View class="flex-1" onLayout={({ bounds }) => belowSections.set(Math.max(0, windowSize().height - bounds.origin.y - bounds.size.height))}><Stack /></View>
      <Overlays bottomInset={belowSections} />
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
  if (!web) return <Onboarding onContinue={() => goHome()} onSettings={() => openPage({ name: "computer" })} />;
  const join = createNavStack<{ name: "join" } | { name: "pair" }>([{ name: "join" }]);
  return (
    <Paged nav={join}>
      <NavStack state={join} style={{ flex: 1 }}>
        {(route) => (route.name === "join" ? <BrowserJoin onPairingLink={() => join.push({ name: "pair" })} /> : <PairPage />)}
      </NavStack>
    </Paged>
  );
}
