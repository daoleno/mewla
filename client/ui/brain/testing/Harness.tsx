import { host, mount, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import type { BrainSnapshot } from "../../../core/brain";
import type { Chat } from "../../../core/conversation/chat";
import { installPlatform } from "../../../core/platform";
import { nshPlatform } from "../../platform";
import { connection, current, servers } from "../../session";
import { FakeDaemon, testServer } from "../../more/testing/fakeDaemon";
import { BrainHostSheet } from "../BrainHostSheet";
import { BrainMenu } from "../BrainMenu";
import { createBrainHostSwitch } from "../hostSwitch";
import type { BrainSource } from "../source";

/**
 * Brain's components on their own, for `nsh test`; the test plays the computer.
 * `host.params`: `view` ("menu" or "host"), `brain` (the snapshot), `threadLoaded`. Its host handler `app.daemon`
 * answers each request (`{type, body}` → the reply's fields, or `{error}` to fail
 * it); `app.snapshot` (an event) gives the next snapshot. What a callback hears
 * shows as text in `harness-out`, one line each.
 */
type Params = { view?: string; brain?: BrainSnapshot; threadLoaded?: boolean };
const params = host.params as Params;
installPlatform(nshPlatform());
const server = testServer();
servers.set([server]);
current.set(server);
connection.set("connected");

const fake = new FakeDaemon({}, server, "connected");
fake.request = (async (type: string, body: Record<string, unknown>, options: { reply: string }) => {
  fake.asked.push({ type, body });
  const answer = (await host.call("app.daemon", { type, body })) as Record<string, unknown> | null;
  if (answer && typeof answer.error === "string") throw Object.assign(new Error(answer.error), { code: answer.code });
  return { type: options.reply, ...(answer ?? {}) };
}) as FakeDaemon["request"];

const snapshot = signal<BrainSnapshot | null>(params.brain ?? null);
host.on<BrainSnapshot>("app.snapshot", (next) => snapshot.set(next));
const out = signal<string[]>([]);
const say = (line: string) => out.set([...untracked(out), line]);
const open = signal(false);

function Out() {
  return (
    <View class="gap-2">
      <Button testID="harness-open" onPress={() => open.set(true)}><Text>Open</Text></Button>
      <Text testID="harness-out">{`out:${out().join(" | ")}`}</Text>
      <Text testID="harness-open-state">{open() ? "open" : "closed"}</Text>
    </View>
  );
}

const chat = { send: (value: string, attachments: { path: string }[] = []) => say(`send:${value}${attachments.length ? ` +${attachments.map((a) => a.path).join(",")}` : ""}`), reload: () => say("reload") } as unknown as Chat;

function Harness() {
  const source: BrainSource = {
    snapshot,
    connected: () => true,
    transport: () => fake,
    chat: () => chat,
    threadLoaded: () => params.threadLoaded ?? true,
    hostSwitch: createBrainHostSwitch({ snapshot, transport: () => fake, serverId: () => server.id, onSwitched: () => say("switched") }),
  };
  const view = params.view ?? "menu";
  return (
    <View class="flex-1 gap-4">
      <Out />
      {view === "menu" ? (
        <BrainMenu open={open} onClose={() => open.set(false)} source={source}
          onOpenTerminal={(id) => say(`terminal:${id}`)} onBrowseWorkspace={() => say("workspace")} onNotice={(text) => say(`notice:${text}`)} />
      ) : view === "host" ? (
        <BrainHostSheet open={open} onClose={() => open.set(false)} source={source} />
      ) : null}
    </View>
  );
}

mount(() => (
  <View class="flex-1 bg-background p-4">
    <Harness />
  </View>
));
