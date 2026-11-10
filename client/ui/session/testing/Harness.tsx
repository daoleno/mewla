import { host, mount, signal, Text, View } from "nsh";
import { Button, Input } from "nsh/base";
import type { CodexSlashCommand } from "../../../core/session/codexSlashCommands";
import { chatSlashCommands } from "../../../core/session/sessionRequests";
import type { AgentKind } from "../../../core/sessions/workerPresentation";
import type { ConnectionState } from "../../../core/transport";
import { FakeDaemon } from "../../more/testing/fakeDaemon";
import { CodexSkillsSheet } from "../CodexSkillsSheet";
import { CodexStatusSheet } from "../CodexStatusSheet";
import { ModelSheet } from "../ModelSheet";
import { ComposerPlusButton, routeComposerPick, routeComposerSend, showComposerAlert, SlashMenu, type ComposerSendRoute } from "../SlashMenu";

/**
 * One Session sheet (or the slash menu) on its own, for `nsh test`: the test plays the
 * computer. Its host handler `app.daemon` answers each request (`{type, body}` → the
 * reply's fields, or `{error, code}` to fail it). `host.params`: `sheet` ("model",
 * "status", "skills" or "slash"), `workerId`, `cwd`, `agentKind`, `state`.
 * What the component reports shows as text: `harness-out`.
 */
type Params = { sheet?: "model" | "status" | "skills" | "slash"; workerId?: string; cwd?: string; agentKind?: AgentKind; state?: ConnectionState };
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

function SlashHarness() {
  const text = signal("");
  const pinned = signal(false);
  const commands = signal<CodexSlashCommand[]>([]);
  const unavailable = signal(false);
  const agentKind = params.agentKind ?? "codex";
  chatSlashCommands(fake).then((list) => commands.set(list), () => unavailable.set(true));
  const apply = async (route: ComposerSendRoute) => {
    pinned.set(false);
    out.set(JSON.stringify(route.kind === "send" ? { kind: route.kind, text: route.text } : route.kind === "prefill" ? { kind: route.kind, text: route.text, alert: route.alert?.title } : route.kind === "alert" ? { kind: route.kind, title: route.title } : route));
    if (route.kind === "prefill") text.set(route.text);
    const alert = route.kind === "alert" ? route : route.kind === "prefill" ? route.alert : undefined;
    if (alert) out.set(`${out()} pressed:${await showComposerAlert(alert)}`);
  };
  const ctx = () => ({ agentKind, slashCommands: commands(), canOpenTerminal: true });
  return (
    <View class="gap-2">
      <SlashMenu text={text} commands={commands} commandsUnavailable={unavailable} agentKind={() => agentKind} connected={() => fake.state === "connected"} pinned={pinned}
        onPick={(command) => void apply(routeComposerPick(command, ctx()))} />
      <View class="flex-row items-center gap-2">
        <ComposerPlusButton pinned={pinned} connected={() => true} onPress={() => pinned.set(!pinned())} />
        <Input testID="harness-text" value={text()} onChange={({ value }) => text.set(value)} class="flex-1" />
        <Button testID="harness-send" onPress={() => void apply(routeComposerSend(text(), ctx()))}><Text>Send</Text></Button>
        <Button testID="harness-clear" onPress={() => text.set("")}><Text>Clear</Text></Button>
      </View>
      <Text testID="harness-out">{out()}</Text>
      <Text testID="harness-count">{`commands:${commands().length}`}</Text>
    </View>
  );
}

mount(() => (
  <View class="flex-1 bg-background p-4">
    {params.sheet === "slash" ? (
      <SlashHarness />
    ) : (
      <View>
        <Opener />
        {params.sheet === "status" ? (
          <CodexStatusSheet open={open} onClose={close} transport={fake} workerId={workerId} onOpenTerminal={() => out.set("terminal")} />
        ) : params.sheet === "skills" ? (
          <CodexSkillsSheet open={open} onClose={close} transport={fake} cwd={params.cwd} onPick={(name) => out.set(`picked:${name}`)} />
        ) : (
          <ModelSheet open={open} onClose={close} transport={fake} workerId={workerId}
            onRuntime={(runtime) => out.set(`runtime:${runtime.model_id}:${runtime.reasoning_effort ?? ""}`)} />
        )}
      </View>
    )}
  </View>
));
