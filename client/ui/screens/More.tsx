import { Text, View } from "nsh";
import { Button, NavPage } from "nsh/base";
import type { NavStackState } from "nsh/base";
import { connection, current, problem, servers, switchTo } from "../session";
import type { MoreRoute } from "../shell/routes";

const destinations: { title: string; route: MoreRoute }[] = [
  { title: "Screen", route: { name: "stub", title: "Screen" } },
  { title: "Extensions", route: { name: "stub", title: "Extensions" } },
  { title: "Stats", route: { name: "stub", title: "Stats" } },
  { title: "Resources", route: { name: "stub", title: "Resources" } },
  { title: "Settings", route: { name: "settings" } },
];

function Row(props: { title: string; detail?: string; onPress: () => void; testID?: string }) {
  return (
    <Button testID={props.testID} onPress={props.onPress} class="flex-row items-center justify-between border-b border-border bg-card px-4 py-3.5 active:bg-accent">
      <Text class="text-base">{props.title}</Text>
      {props.detail ? <Text class="text-base text-muted-foreground">{props.detail}</Text> : null}
    </Button>
  );
}

/** The connection line: the menu footer of the previous app, one quiet line naming the computer and its state. */
const WORDS = { connected: "Connected", connecting: "Connecting…", waiting: "Offline", idle: "Not connected" } as const;

/** The current computer and its connection. Signals are read inside the JSX so the line follows them. */
export function ConnectionLine() {
  return (
    <View class="flex-row items-center gap-2 px-4 py-4">
      <View class={connection() === "connected" ? "size-2 rounded-full bg-muted-foreground" : "size-2 rounded-full bg-warning"} />
      <Text testID="connection" class="text-sm text-muted-foreground">
        {`${current()?.name || "No computer"} · ${WORDS[connection()]}${connection() === "waiting" && problem() ? ` (${problem()})` : ""}`}
      </Text>
    </View>
  );
}

export function More(props: { nav: NavStackState<MoreRoute> }) {
  return (
    <NavPage title="More">
      <View class="flex-1 bg-background">
        <View class="mt-4 border-t border-border">
          {destinations.map((d) => <Row testID={`more-${d.title}`} title={d.title} onPress={() => props.nav.push(d.route)} />)}
        </View>
        <ConnectionLine />
      </View>
    </NavPage>
  );
}

export function Settings(props: { onPair: () => void }) {
  return (
    <NavPage title="Settings">
      <View class="flex-1 bg-background">
        <Text class="px-4 pb-2 pt-6 text-sm text-muted-foreground">Computers</Text>
        <View class="border-t border-border">
          {servers().map((s) => <Row title={s.name || s.url} detail={s.id === current()?.id ? "Current" : undefined} onPress={() => switchTo(s)} />)}
          <Row testID="pair-another" title="Pair a computer" onPress={props.onPair} />
        </View>
      </View>
    </NavPage>
  );
}

export function StubPage(props: { title: string }) {
  return (
    <NavPage title={props.title}>
      <View class="flex-1 items-center justify-center bg-background"><Text class="text-base text-muted-foreground">{`${props.title} arrives in a later preview.`}</Text></View>
    </NavPage>
  );
}
