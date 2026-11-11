import { platform, Text, View } from "nsh";
import { Button, NavPage } from "nsh/base";
import type { NavStackState } from "nsh/base";
import { followFriends, friendsEnabled } from "../more/friends/state";
import { connection, current, problem } from "../session";
import type { MoreRoute } from "../shell/routes";
import { updatePending } from "../update/update";

const destinations: { title: string; route: MoreRoute; labs?: boolean }[] = [
  { title: "Screen", route: { name: "screen" } },
  { title: "Extensions", route: { name: "extensions" } },
  { title: "Stats", route: { name: "stats" } },
  { title: "Resources", route: { name: "resources" } },
  { title: "Friends", route: { name: "friends" }, labs: true },
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

let following = false;

export function More(props: { nav: NavStackState<MoreRoute> }) {
  // Friends is asked about once More is first shown (a hidden tab has no size), not at every start.
  const shown = (size: { width: number; height: number }) => {
    if (following || size.width <= 0 || size.height <= 0) return;
    following = true;
    followFriends();
  };
  return (
    <NavPage title="More">
      <View class="flex-1 bg-background" onLayout={(e) => shown(e.bounds.size)}>
        <View class="mt-4 border-t border-border">
          {/* Friends is a phone Lab: v0.3.1's web app had none. */}
          {destinations.filter((d) => !d.labs || (platform.os !== "web" && friendsEnabled())).map((d) => <Row testID={`more-${d.title}`} title={d.title} detail={d.title === "Settings" && updatePending() ? "Update available" : undefined} onPress={() => props.nav.push(d.route)} />)}
        </View>
        <ConnectionLine />
      </View>
    </NavPage>
  );
}
