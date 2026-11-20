import { signal, Text, untracked, View } from "nsh";
import { Button } from "nsh/base";
import { brainAgentInstalls, type BrainAgentInstall } from "../../core/brain";
import { createSession } from "../../core/sessions/create";
import { agentKindFromProvider } from "../../core/sessions/workerPresentation";
import { AgentIcon } from "../icons/AgentIcon";
import { brain, currentTransport } from "../session";
import { openSession } from "../shell/nav";

/**
 * No agent on this Mac (DESIGN-MAC.md §2.4, `m-noagent`): Brain can't start, and its thread offers the installs this
 * Mac's daemon gives (Install Claude Code, Install Codex). Each runs as a terminal Session on this Mac, its own
 * `mewla deps -agent`: the installer and the sign-in ask there, then Brain moves to that agent and starts.
 */
export function MacNoAgent() {
  const starting = signal("");
  const failed = signal("");
  const install = async (item: BrainAgentInstall) => {
    const transport = untracked(currentTransport);
    if (!transport || untracked(starting)) return;
    starting.set(item.id);
    failed.set("");
    const result = await createSession(transport, { command: item.command, name: `Install ${item.name}` });
    starting.set("");
    if (result.kind === "navigable") openSession(result.workerId);
    else failed.set(result.message);
  };
  return (
    <View testID="mac-no-agent" class="flex-1 items-center justify-center gap-3 py-10">
      <Text role="header" class="text-title font-bold text-foreground">No agent on this Mac</Text>
      <View class="flex-row flex-wrap items-center justify-center gap-2">
        {brainAgentInstalls(brain()).map((item) => (
          <Button testID={`mac-install-${item.id}`} disabled={Boolean(starting())} onPress={() => void install(item)}
            class="h-8 flex-row items-center gap-1.5 rounded-[9px] border border-border bg-background px-3 active:opacity-70 disabled:opacity-40">
            <AgentIcon kind={agentKindFromProvider(item.id)} size={18} slot={20} />
            <Text class="text-sub font-semibold text-foreground">{`Install ${item.name}`}</Text>
          </Button>
        ))}
      </View>
      <If when={failed()}><Text testID="mac-install-failed" class="text-sub text-muted-foreground">{failed()}</Text></If>
    </View>
  );
}
