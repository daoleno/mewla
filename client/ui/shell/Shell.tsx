import { host, platform, signal, Text, View, windowSize } from "nsh";
import { Button, createNavStack, NavStack } from "nsh/base";
import { isAvailable as mobile, TabBar } from "nsh/mobile";
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
import { onTransport } from "../session";
import type { TerminalRouteSessionHint } from "../../core/session/sessionRoute";
import type { MoreRoute, SessionsRoute } from "./routes";

const tabs = [
  { title: "Brain", ios: "sparkles", android: "tab_brain" },
  { title: "Sessions", ios: "list.bullet", android: "tab_sessions" },
  { title: "Calendar", ios: "calendar", android: "tab_calendar" },
  { title: "More", ios: "ellipsis", android: "tab_more" },
];

export const selectedTab = signal(0);
/** What lies below the sections (the TabBar, the system bar): the keyboard covers it before it reaches a page. */
export const belowSections = signal(0);

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
  sessionsNav.setRoutes([{ name: "sessions" }, { name: "session", workerId, hint }]);
}

/**
 * How a Session's Git review and file previews push their next page: theirs stay on the
 * Sessions stack, anything else opens under More.
 */
const sessionsPageNav: PageNav = {
  push(route: MorePage) {
    if (route.name.startsWith("git") || route.name.startsWith("files-")) sessionsNav.push(route as SessionsRoute);
    else {
      selectedTab.set(3);
      moreNav.setRoutes([{ name: "more" }, route]);
    }
  },
  pop: () => sessionsNav.pop(),
};

function sessionsRoute(route: SessionsRoute, index: () => number) {
  switch (route.name) {
    case "sessions":
      return <Sessions nav={sessionsNav} onNew={() => newSessionOpen.set(true)} onPair={() => {
        selectedTab.set(3);
        moreNav.setRoutes([{ name: "more" }, { name: "settings" }, { name: "pair" }]);
      }} />;
    case "session":
      return <Session nav={sessionsNav} workerId={route.workerId} hint={route.hint}
        active={() => selectedTab() === 1 && index() === sessionsNav.routes().length - 1} />;
    case "workFile":
      return <WorkFilePage id={route.id} onClose={() => sessionsNav.pop()} openCalendarItem={(id) => {
        showCalendarItem(id);
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

/** One native stack per tab; every stack stays mounted so a tab keeps its place. */
function Section(props: { index: number }) {
  const shown = () => selectedTab() === props.index;
  return (
    <View class="absolute inset-0" style={{ display: shown() ? "flex" : "none" }}>
      {props.index === 0 ? <NavStack state={brainNav} style={{ flex: 1 }}>{(route) => (route.name === "brain" ? <Brain /> : workRoute(route, brainNav))}</NavStack>
        : props.index === 1 ? <NavStack state={sessionsNav} style={{ flex: 1 }}>{sessionsRoute}</NavStack>
        : props.index === 2 ? <NavStack state={calendarNav} style={{ flex: 1 }}>{calendarRoute}</NavStack>
        : <NavStack state={moreNav} style={{ flex: 1 }}>{moreRoute}</NavStack>}
    </View>
  );
}

export function Shell() {
  if (!mobile) {
    return (
      // Web and desktop get their wide layout in M5; until then a plain row of sections.
      <View class="flex-1 bg-background">
        <View class="flex-row gap-1 border-b border-border px-2">
          {tabs.map((t, i) => (
            <Button onPress={() => selectedTab.set(i)} class="px-3 py-2.5">
              <Text class={selectedTab() === i ? "text-base font-semibold" : "text-base text-muted-foreground"}>{t.title}</Text>
            </Button>
          ))}
        </View>
        <View class="flex-1">{tabs.map((_, i) => <Section index={i} />)}</View>
        <NewSession onCreated={(workerId, fromSession, hint) => (fromSession ? sessionsNav.replace({ name: "session", workerId, hint }) : openSession(workerId, hint))} />
        <ServicesSheet onOpenSession={(workerId) => openSession(workerId)} />
        <EnrollmentApproval />
      </View>
    );
  }
  return (
    <View class="flex-1 bg-background">
      <View class="flex-1" onLayout={({ bounds }) => belowSections.set(Math.max(0, windowSize().height - bounds.origin.y - bounds.size.height))}>{tabs.map((_, i) => <Section index={i} />)}</View>
      <NewSession onCreated={(workerId, fromSession, hint) => (fromSession ? sessionsNav.replace({ name: "session", workerId, hint }) : openSession(workerId, hint))} />
        <ServicesSheet onOpenSession={(workerId) => openSession(workerId)} />
        <EnrollmentApproval />
      <TabBar testID="tabs" items={tabs.map((t) => ({ title: t.title, icon: platform.select({ ios: t.ios, android: t.android, default: t.android }) }))}
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
    <NavStack state={nav} style={{ flex: 1 }}>
      {(route) => (route.name === "join" ? <BrowserJoin onPairingLink={() => nav.push({ name: "pair" })} /> : <PairPage />)}
    </NavStack>
  );
}
