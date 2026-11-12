import { Text, View } from "nsh";
import { connection, current, problem } from "../session";

const WORDS = { connected: "Connected", connecting: "Connecting…", waiting: "Offline", idle: "Not connected" } as const;

/** One quiet line naming the current computer and its connection (the desktop sidebar's footer). */
export function ConnectionLine() {
  return (
    <View class="flex-row items-center gap-2 px-4 py-4">
      <View class={connection() === "connected" ? "size-2 rounded-full bg-add" : "size-2 rounded-full border-2 border-muted-foreground"} />
      <Text testID="connection" class="text-sub text-muted-foreground">
        {`${current()?.name || "No computer"} · ${WORDS[connection()]}${connection() === "waiting" && problem() ? ` (${problem()})` : ""}`}
      </Text>
    </View>
  );
}
