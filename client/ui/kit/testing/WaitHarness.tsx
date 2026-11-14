import { mount, signal, Text, View } from "nsh";
import { PillButton } from "../kit";
import { forceWait, linkState, Rows, states, StateView, useWait, WaitCell } from "../wait";

/**
 * One place waiting, for `nsh test`: a list read by a fake computer the test answers with the buttons
 * (answer, empty, error), the link the test sets, a forced state, and a button that waits.
 */
/** The content counts how often it is made: a wait must never re-make it. */
const made = signal(0);
function Content() {
  made.set(made() + 1);
  return <Text testID="content">{`Pricing page rework · made ${made()}`}</Text>;
}

function Harness() {
  let reads = 0;
  const readCount = signal(0);
  const wait = useWait({ read: () => readCount.set(++reads) });
  const busy = signal(false);
  wait.start();
  return (
    <View class="flex-1 bg-background pt-10">
      <View class="flex-row flex-wrap gap-1">
        <Text testID="answer" onPress={() => wait.resolve("content")}>answer</Text>
        <Text testID="answer-empty" onPress={() => wait.resolve("empty")}>empty</Text>
        <Text testID="answer-error" onPress={() => wait.fail()}>error</Text>
        <Text testID="link-up" onPress={() => linkState.set("up")}>up</Text>
        <Text testID="link-connecting" onPress={() => linkState.set("connecting")}>connecting</Text>
        <Text testID="link-away" onPress={() => linkState.set("away")}>away</Text>
        <Text testID="force-slow" onPress={() => forceWait("slow")}>slow</Text>
        <Text testID="force-off" onPress={() => forceWait(null)}>off</Text>
        <Text testID="reads">{`reads ${readCount()}`}</Text>
      </View>
      <WaitCell
        wait={wait}
        placeholder={() => <Rows count={3} />}
        failed={() => <StateView {...states.failed("sessions", "Desk", wait.retry)} />}
        away={() => <StateView {...states.away("Desk", "13:40", wait.retry)} />}
        empty={() => <StateView icon="sessions" title="No sessions" testID="wait-empty" />}
      >
        <Content />
      </WaitCell>
      <PillButton testID="approve" title="Approve" kind="seal" busy={busy()} onPress={() => busy.set(true)} />
      <Text testID="approve-done" onPress={() => busy.set(false)}>done</Text>
    </View>
  );
}

linkState.set("up");
mount(Harness);
