import { host, mount, signal, Text, View } from "nsh";
import { Button } from "nsh/base";
import type { ConnectionState } from "../../../core/transport";
import { FakeDaemon } from "../../more/testing/fakeDaemon";
import { ModelSheet } from "../ModelSheet";

/**
 * A Session's Model sheet on its own, for `nsh test`: the test plays the computer. Its
 * host handler `app.daemon` answers each request (`{type, body}` → the reply's fields,
 * or `{error, code}` to fail it). `host.params`: `workerId`, `state`. What the
 * component reports shows as text: `harness-out`.
 */
type Params = { workerId?: string; state?: ConnectionState };
const params = host.params as Params;
const fake = new FakeDaemon({}, undefined, params.state ?? "connected");
fake.request = (async (type: string, body: Record<string, unknown>, options: { reply: string }) => {
  fake.asked.push({ type, body });
  if (fake.state !== "connected") throw new Error("Daemon is not connected.");
  const answer = (await host.call("app.daemon", { type, body })) as Record<string, unknown> | null;
  if (answer && typeof answer.error === "string") throw Object.assign(new Error(answer.error), { code: answer.code });
  return { type: options.reply, ...(answer ?? {}) };
}) as FakeDaemon["request"];

const workerId = params.workerId ?? "w1";
const open = signal(false);
const out = signal("");
const closes = signal(0);
const close = () => {
  open.set(false);
  closes.set(closes() + 1);
};

function Opener() {
  return (
    <View class="gap-2">
      <Button testID="harness-open" onPress={() => open.set(true)}><Text>Open</Text></Button>
      <Text testID="harness-out">{out()}</Text>
      <Text testID="harness-closes">{`closes:${closes()}`}</Text>
    </View>
  );
}

mount(() => (
  <View class="flex-1 bg-background p-4">
    <Opener />
    <ModelSheet open={open} onClose={close} transport={fake} workerId={workerId}
      onRuntime={(runtime) => out.set(`runtime:${runtime.model_id}:${runtime.reasoning_effort ?? ""}`)} />
  </View>
));
