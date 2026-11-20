import { Text, View } from "nsh";
import { brainExecutorChoices, brainExecutorLabel } from "../../core/brainMenu";
import { agentKindFromProvider } from "../../core/sessions/workerPresentation";
import { AgentIcon } from "../icons/AgentIcon";
import { brain } from "../session";

/**
 * First run on a Mac (DESIGN-MAC.md §2.4, `m-ready`): Brain's empty thread says "Ready when you are" over the agents
 * this Mac has (logo and name, Brain's host first). It is the empty thread's, so it goes with the first message.
 */
export function MacReady() {
  const agents = () => {
    const snapshot = brain();
    const { choices } = brainExecutorChoices(snapshot?.executors ?? [], snapshot?.executor_states);
    const host = snapshot?.host_executor?.id;
    return [...choices.filter((agent) => agent.id === host), ...choices.filter((agent) => agent.id !== host)];
  };
  return (
    <View testID="mac-ready" class="flex-1 items-center justify-center gap-3 py-10">
      <Text role="header" class="text-title font-bold text-foreground">Ready when you are</Text>
      <If when={agents().length > 0}>
        <View class="flex-row flex-wrap items-center justify-center" style={{ columnGap: 14, rowGap: 6 }}>
          {agents().map((agent) => (
            <View testID={`mac-ready-${agent.id}`} class="flex-row items-center gap-1.5">
              <AgentIcon kind={agentKindFromProvider(agent.provider)} size={18} slot={20} />
              <Text class="text-body text-muted-foreground">{brainExecutorLabel(agent)}</Text>
            </View>
          ))}
        </View>
      </If>
    </View>
  );
}
