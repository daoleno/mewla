import { platform, signal, Text, View } from "nsh";
import { Button, createNavStack, NavStack } from "nsh/base";
import { isAvailable as mobile, TabBar } from "nsh/mobile";
import { Onboarding } from "../screens/Onboarding";
import { More, Settings, StubPage } from "../screens/More";
import { Brain, Calendar, Sessions } from "../screens/Placeholders";
import type { MoreRoute } from "./routes";

const tabs = [
  { title: "Brain", ios: "sparkles", android: "tab_brain" },
  { title: "Sessions", ios: "list.bullet", android: "tab_sessions" },
  { title: "Calendar", ios: "calendar", android: "tab_calendar" },
  { title: "More", ios: "ellipsis", android: "tab_more" },
];

export const selectedTab = signal(0);

/** The section a `mewla://` link opens: `mewla://brain`, `…/sessions`, `…/calendar`, `…/more`. */
export function sectionOfLink(url: string): number | null {
  const match = /^mewla:\/\/([a-z]+)/i.exec(url.trim());
  const index = match ? ["brain", "sessions", "calendar", "more"].indexOf(match[1].toLowerCase()) : -1;
  return index < 0 ? null : index;
}
const moreNav = createNavStack<MoreRoute>([{ name: "more" }]);
const brainNav = createNavStack<{ name: "brain" }>([{ name: "brain" }]);
const sessionsNav = createNavStack<{ name: "sessions" }>([{ name: "sessions" }]);
const calendarNav = createNavStack<{ name: "calendar" }>([{ name: "calendar" }]);

function moreRoute(route: MoreRoute) {
  switch (route.name) {
    case "more": return <More nav={moreNav} />;
    case "settings": return <Settings onPair={() => moreNav.push({ name: "pair" })} />;
    case "pair": return <Onboarding />;
    case "stub": return <StubPage title={route.title} />;
  }
}

/** One native stack per tab; every stack stays mounted so a tab keeps its place. */
function Section(props: { index: number }) {
  const shown = () => selectedTab() === props.index;
  return (
    <View class="absolute inset-0" style={{ display: shown() ? "flex" : "none" }}>
      {props.index === 0 ? <NavStack state={brainNav} style={{ flex: 1 }}>{() => <Brain />}</NavStack>
        : props.index === 1 ? <NavStack state={sessionsNav} style={{ flex: 1 }}>{() => <Sessions />}</NavStack>
        : props.index === 2 ? <NavStack state={calendarNav} style={{ flex: 1 }}>{() => <Calendar />}</NavStack>
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
      </View>
    );
  }
  return (
    <View class="flex-1 bg-background">
      <View class="flex-1">{tabs.map((_, i) => <Section index={i} />)}</View>
      <TabBar testID="tabs" items={tabs.map((t) => ({ title: t.title, icon: platform.select({ ios: t.ios, android: t.android, default: t.android }) }))}
        selectedIndex={selectedTab()} onChange={({ index }) => selectedTab.set(index)} />
    </View>
  );
}

export function FirstRun() {
  const nav = createNavStack<{ name: "pair" }>([{ name: "pair" }]);
  return <NavStack state={nav} style={{ flex: 1 }}>{() => <Onboarding />}</NavStack>;
}
