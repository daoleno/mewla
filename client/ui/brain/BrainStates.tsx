import { Text, View } from "nsh";
import type { JSX } from "nsh";
import { PillButton } from "../kit/kit";
import { Icon } from "../kit/Icon";
import { brainExecutorChoices, brainExecutorLabel, canSwitchBrainExecutor, canSwitchBrainExecutorFromProblem } from "../../core/brainMenu";
import { currentBrain, type BrainSource } from "./source";

/** The centred place in Brain's thread area that says what is wrong, and its ways out. */
function ThreadState(props: { testID: string; title: string; children?: JSX.Element }) {
  return (
    <View testID={props.testID} class="flex-1 items-center justify-center gap-2 px-4">
      <Text class="text-center text-body font-semibold text-foreground">{props.title}</Text>
      <View class="flex-row flex-wrap justify-center gap-2">{props.children ?? null}</View>
    </View>
  );
}

/**
 * Brain runs on an agent without chat events (soft-final/SPEC.md §1.3): "Chat isn't
 * available for {agent}", Open terminal and Switch executor (when another agent is
 * installed). Its composer is disabled meanwhile ("Chat isn't available",
 * `brainComposerBlock`).
 */
export function BrainChatUnavailable(props: { onOpenTerminal?: (hostWorkerId: string) => void; onSwitchExecutor?: () => void; source?: BrainSource }) {
  const source = props.source ?? currentBrain;
  const agent = () => brainExecutorLabel(source.snapshot()?.host_executor) || "this agent";
  return (
    <ThreadState testID="brain-chat-unavailable" title={`Chat isn't available for ${agent()}`}>
      <PillButton testID="brain-open-terminal" title="Open terminal" kind="ink" small onPress={() => props.onOpenTerminal?.(source.snapshot()?.host_worker?.id ?? "")} />
      <If when={canSwitchBrainExecutor(source.snapshot())}><PillButton testID="brain-switch-executor" title="Switch executor" small onCanvas onPress={() => props.onSwitchExecutor?.()} /></If>
    </ThreadState>
  );
}

/**
 * Brain's chat did not load: an error, or a transcript the computer cannot read
 * (soft-final/SPEC.md §1.3, `brainChatState` "failed"): "Couldn't load the chat",
 * Try again (reads it again) and Open terminal. Its composer is disabled meanwhile
 * ("Chat didn't load").
 */
export function BrainChatFailed(props: { onTryAgain?: () => void; onOpenTerminal?: (hostWorkerId: string) => void; source?: BrainSource }) {
  const source = props.source ?? currentBrain;
  return (
    <ThreadState testID="brain-chat-failed" title="Couldn't load the chat">
      <PillButton testID="brain-chat-try-again" title="Try again" kind="ink" small onPress={() => (props.onTryAgain ? props.onTryAgain() : source.chat()?.reload())} />
      <PillButton testID="brain-chat-open-terminal" title="Open terminal" small onCanvas onPress={() => props.onOpenTerminal?.(source.snapshot()?.host_worker?.id ?? "")} />
    </ThreadState>
  );
}

/**
 * "Brain can't start" (soft-final/SPEC.md §1.3, the tray's notice): the host
 * problem as the computer reports it, then Use {suggested} (an installed agent the
 * computer verified as signed in; it switches in place, "Switching…") and Switch
 * executor (when another agent is installed). A refused switch shows under the
 * buttons. Renders nothing while Brain has no host problem.
 */
export function BrainCantStartNotice(props: { onSwitchExecutor?: () => void; source?: BrainSource }) {
  const source = props.source ?? currentBrain;
  const problem = () => source.snapshot()?.host_problem;
  const suggested = () => {
    const id = problem()?.suggest_executor;
    return id ? brainExecutorChoices(source.snapshot()?.executors ?? [], source.snapshot()?.executor_states).choices.find((executor) => executor.id === id) : undefined;
  };
  const canSwitch = () => Boolean(problem() && canSwitchBrainExecutor(source.snapshot()) && canSwitchBrainExecutorFromProblem(problem()!));
  return (
    <View>
      <If when={problem()}><View testID="brain-cant-start" class="min-h-[68px] gap-3 rounded-(--radius-group) bg-card px-4 py-3.5">
          <View class="flex-row items-start gap-3.5">
            <Icon name="alert-circle" size={24} tone="soft" />
            <View class="flex-1">
              <Text class="text-body font-semibold">Brain can't start</Text>
              <Text class="mt-0.5 text-sub text-muted-foreground">{problem()!.detail}</Text>
            </View>
          </View>
          <If when={suggested() || canSwitch()}><View class="flex-row flex-wrap gap-2 pl-[38px]">
              <If when={suggested()}><PillButton testID="brain-use-suggested" kind="ink" small title={source.hostSwitch.switching() ? "Switching…" : `Use ${brainExecutorLabel(suggested())}`}
                  disabled={Boolean(source.hostSwitch.switching()) || !source.connected()} onPress={() => void source.hostSwitch.switchTo(suggested()!)} /></If>
              <If when={canSwitch()}><PillButton testID="brain-switch-executor" title="Switch executor" kind={suggested() ? "ghost" : "ink"} small onPress={() => props.onSwitchExecutor?.()} /></If>
            </View></If>
          <If when={source.hostSwitch.error()}><Text testID="brain-cant-start-error" class="pl-[38px] text-sub text-foreground">{source.hostSwitch.error()}</Text></If>
        </View></If>
    </View>
  );
}
