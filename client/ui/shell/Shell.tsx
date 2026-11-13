import { computed, effect, host, platform, signal, untracked, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { createNavStack, NavStack, type NavStackState } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { PageStack } from "./NavPage";
import { reuseRoutes, type PoppedPage } from "../../core/web/history";
import type { WebPlace } from "../../core/web/places";
import { placeOfRoutes, routesOfPlace } from "./routePlaces";
import { DesktopOverlays } from "./desktop/Overlays";
import { LeftPane, PaneHandle } from "./LeftPane";
import { loadPaneWidth, paneWidth, rightPaneWidth, wide } from "./layout";
import { MenuHost } from "../kit/Menu";
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
import { goHome, isTop, nav, openPage, openSession, pageNav, trayOpen } from "./nav";
import type { Route } from "./routes";

/** What lies below the stack (the system bar): the keyboard covers it before it reaches a page. */
export const belowSections = signal(0);

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

/** Goes to a place a URL, the palette or `G` then a letter names. */
export function openPlace(place: WebPlace) {
  nav.setRoutes([{ name: "home" }, ...routesOfPlace(place)]);
}

/** Where the person is: what the URL and the tab title follow. */
export const currentPlace = computed<WebPlace>(() => placeOfRoutes(nav.routes()));

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
 * A page learns whether there is a Back (the stack holds more than home) and what Back does: the
 * desktops and the web draw their own header, and on iOS and Android Back carries the connection
 * mark itself (SPEC §1.3). Around the stack, not each page: a NavStack's page must be its NavPage.
 */
function Paged<R>(props: { nav: NavStackState<R>; children: JSX.Element }) {
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

/** What every layout lays over the stack. The floating screen stays in the right pane, at its bottom right on wide. */
function Overlays(props: { bottomInset?: () => number }) {
  return (
    <>
      <ScreenFloatHost page={floatPage} onOpenPhone={openPhonePage} onOpenScreen={openScreenPage} bottomInset={props.bottomInset} leftInset={() => (wide() ? paneWidth() : 0)} />
      <NewSession onCreated={(workerId, fromSession, hint) => (fromSession ? nav.replace({ name: "session", workerId, hint }) : openSession(workerId, hint))} />
      <ServicesSheet onOpenSession={(workerId) => openSession(workerId)} />
      <EnrollmentApproval />
      <CalendarAlertCard />
    </>
  );
}

/**
 * The app's frame (soft-final SPEC §14): one tree at every width. From a 1024 pt window, on any
 * device, the tray is the left pane beside the one stack; below it the stack alone, as on a phone.
 * Crossing 1024 only adds or removes the pane: the stack, its pages, their scroll and what is typed
 * stay as they were.
 */
export function Shell() {
  loadPaneWidth();
  // On the desktops and the web every place has a URL and the tab a title (SPEC §14.5).
  if (!mobile) syncWebLocation({ place: currentPlace, snapshot, pageTitle, serverId: () => current()?.id ?? null, restore });
  // Narrow to wide: an open tray becomes the left pane (§14.6), and the phone's overlay does not come back when narrowed again.
  effect(() => {
    if (wide()) untracked(() => trayOpen() && trayOpen.set(false));
  });
  return (
    <View class="flex-1 bg-background">
      <View class="flex-1 flex-row">
        <If when={wide()}><LeftPane /></If>
        <If when={wide()}><PaneHandle /></If>
        <View testID="right-pane" class="flex-1" onLayout={({ bounds }) => {
          rightPaneWidth.set(bounds.size.width);
          belowSections.set(Math.max(0, windowSize().height - bounds.origin.y - bounds.size.height));
        }}><Stack /></View>
      </View>
      <Overlays bottomInset={belowSections} />
      <DesktopOverlays />
      <MenuHost />
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
        {(route) => (<If when={route.name === "join"} else={<PairPage />}><BrowserJoin onPairingLink={() => join.push({ name: "pair" })} /></If>)}
      </NavStack>
    </Paged>
  );
}
