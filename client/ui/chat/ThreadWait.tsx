import type { JSX } from "nsh";
import { effect, untracked, View } from "nsh";
import { StateView, states, Thread, useWait, WaitCell } from "../kit/wait";

/**
 * What a thread (Brain's, a Session's) shows while it has no rows (SPEC §16.4): nothing under `wait.quiet`,
 * then the placeholder thread held on the bottom, the slow line from `wait.slow`, the failed state at
 * `wait.fail` with Try again (and the thread's own second action), the computer away at `link.away`; its
 * empty words only after a read that succeeded. The rows themselves are the list's, never this.
 */
export function ThreadWait(props: {
  /** The thread is being read (or the computer is still connecting). */
  waiting: () => boolean;
  /** What the thread says once read with nothing in it ("What should Brain do?"). */
  empty: () => JSX.Element;
  /** Reads the thread again (Try again, the link back). */
  reload: () => void;
  /** The link's own Try again while the computer is away. */
  reconnect?: () => void;
  computer: () => string | undefined;
  lastSeen?: () => string | undefined;
  /** The second action of the failed state ("Open terminal"). */
  failedAction?: { title: string; onPress: () => void; testID?: string };
  /** The rows arrived (the list leaves this slot). */
  hasRows?: () => boolean;
  testID?: string;
}) {
  const wait = useWait({ read: props.reload, reconnect: props.reconnect });
  effect(() => {
    const waiting = props.waiting();
    untracked(() => {
      if (waiting) {
        if (!wait.reading()) wait.start();
      } else wait.resolve(props.hasRows?.() ? "content" : "empty");
    });
  });
  const failed = () => {
    const s = states.failed("the chat", props.computer(), wait.retry);
    return <StateView {...s} title="Couldn't load the chat" actions={[...(s.actions ?? []), ...(props.failedAction ? [props.failedAction] : [])]} testID="thread-failed" />;
  };
  return (
    <View testID={props.testID ?? "thread-wait"} class="flex-1">
      <WaitCell wait={wait} bottom class="flex-1" placeholder={() => <Thread />} failed={() => <View class="flex-1 justify-center">{failed()}</View>}
        away={() => <View class="flex-1 justify-center"><StateView {...states.away(props.computer(), props.lastSeen?.(), wait.retry)} /></View>}
        empty={() => <View class="flex-1">{props.empty()}</View>}>
        <View />
      </WaitCell>
    </View>
  );
}
