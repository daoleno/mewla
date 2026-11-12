import { host, mount, signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import type { BrainSnapshot } from "../../../core/brain";
import type { Chat } from "../../../core/conversation/chat";
import { installPlatform } from "../../../core/platform";
import { nshPlatform } from "../../platform";
import { connection, current, servers } from "../../session";
import { Composer, createComposerText } from "../../chat/Composer";
import { AttachmentTray } from "../../more/files/Pages";
import { FakeDaemon, testServer } from "../../more/testing/fakeDaemon";
import { CodexStatusSheet } from "../../session/CodexStatusSheet";
import { ComposerPlusButton, SlashMenu } from "../../session/SlashMenu";
import { BrainHostSheet } from "../BrainHostSheet";
import { BrainMenu } from "../BrainMenu";
import { BrainCantStartNotice, BrainChatFailed, BrainChatUnavailable } from "../BrainStates";
import { createBrainComposer } from "../composer";
import { createBrainHostSwitch } from "../hostSwitch";
import type { BrainSource } from "../source";
import { MentionPanel } from "../MentionPanel";

/**
 * Brain's components on their own, for `nsh test`; the test plays the computer.
 * `host.params`: `view` ("menu", "host", "cant-start", "unavailable", "failed" or
 * "composer"), `brain` (the snapshot), `threadLoaded`, `blocked` (the composer's disabled reason). Its host handler `app.daemon`
 * answers each request (`{type, body}` → the reply's fields, or `{error}` to fail
 * it); `app.snapshot` (an event) gives the next snapshot. What a callback hears
 * shows as text in `harness-out`, one line each.
 */
type Params = { view?: string; brain?: BrainSnapshot; threadLoaded?: boolean; blocked?: string };
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
  if (view === "composer") return <ComposerHarness source={source} />;
  return (
    <View class="flex-1 gap-4">
      <Out />
      {view === "menu" ? (
        <BrainMenu open={open} onClose={() => open.set(false)} source={source}
          onOpenTerminal={(id) => say(`terminal:${id}`)} onBrowseWorkspace={() => say("workspace")} onNotice={(text) => say(`notice:${text}`)} />
      ) : view === "host" ? (
        <BrainHostSheet open={open} onClose={() => open.set(false)} source={source} />
      ) : view === "cant-start" ? (
        <View>
          <BrainCantStartNotice source={source} onSwitchExecutor={() => say("switch-executor")} />
        </View>
      ) : view === "unavailable" ? (
        <BrainChatUnavailable source={source} onOpenTerminal={(id) => say(`terminal:${id}`)} onSwitchExecutor={() => say("switch-executor")} />
      ) : (
        <BrainChatFailed source={source} onOpenTerminal={(id) => say(`terminal:${id}`)} />
      )}
    </View>
  );
}

/** Brain's composer with what it hosts: the "+" menu (Attach File, Codex's commands), the files, the `@` list. */
function ComposerHarness(props: { source: BrainSource }) {
  const text = createComposerText(() => null);
  const composer = createBrainComposer({ text, source: props.source, onOpenTerminal: (id) => say(`terminal:${id}`), onNotice: (line) => say(`notice:${line}`) });
  return (
    <View class="flex-1 justify-end">
      <Out />
      <MentionPanel text={text.text} executors={composer.executors} onPick={(draft) => text.set(draft)} />
      <Composer text={text} placeholder="Tell Brain" canSend={composer.canSend} onSend={composer.send}
        running={() => false} stopping={() => false} canStop={() => true} onStop={() => {}} pickerOpen={composer.pickerOpen}
        disabled={() => Boolean(params.blocked)} disabledReason={() => params.blocked ?? ""}
        leading={<ComposerPlusButton pinned={composer.pinned} connected={() => true} onPress={() => composer.pinned.set(!untracked(composer.pinned))} />}>
        <SlashMenu text={text.text} commands={composer.commands} commandsUnavailable={composer.commandsUnavailable} agentKind={composer.kind} connected={() => true}
          pinned={composer.pinned} onPick={composer.pick} onAttach={composer.attach} uploading={composer.uploading} />
        {composer.uploads.items().length > 0 ? <AttachmentTray uploads={composer.uploads} /> : null}
      </Composer>
      <CodexStatusSheet open={composer.statusOpen} onClose={() => composer.statusOpen.set(false)} transport={fake} workerId={snapshot()?.host_worker?.id ?? ""} onOpenTerminal={() => say("terminal")} />
    </View>
  );
}

mount(() => (
  <View class="flex-1 bg-background p-4">
    <Harness />
  </View>
));
