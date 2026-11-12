import { effect, Scroll, Text, untracked, View } from "nsh";
import { Sheet } from "../kit/Sheet";
import { Row } from "../kit/kit";
import { AgentIcon } from "../icons/AgentIcon";
import { agentKindFromProvider } from "../../core/sessions/workerPresentation";
import { brainExecutorChoices, brainExecutorLabel, brainExecutorMeta } from "../../core/brainMenu";
import type { BrainExecutorRef } from "../../core/brain";
import { currentBrain, type BrainSource } from "./source";

/**
 * "Brain host" (soft-final/SPEC.md §1.6 Switch executor): the installed agents,
 * signed-in first, with their provider, runtime and account as the computer reports
 * them; the one Brain runs on is checked; "Install more with mewla deps" when more
 * are configured than installed. A pick switches Brain to it (the source's host
 * switch); the current one closes the page. The computer's refusal shows under it.
 */
export function BrainHostPanel(props: { onClose: () => void; source?: BrainSource }) {
  const source = props.source ?? currentBrain;
  const choices = () => brainExecutorChoices(source.snapshot()?.executors ?? [], source.snapshot()?.executor_states);
  const hostId = () => source.snapshot()?.host_executor?.id ?? "";
  const choose = (executor: BrainExecutorRef) => {
    if (source.hostSwitch.switching()) return;
    if (executor.id === hostId()) return props.onClose();
    void source.hostSwitch.switchTo(executor);
  };
  return (
    <View testID="brain-host-panel" class="gap-3 px-4">
      <Scroll keyboardShouldPersistTaps="handled" style={{ maxHeight: 448 }}>
        <View class="overflow-hidden rounded-(--radius-group) bg-sheet-surface">
          <View class="-mb-px">
            {choices().choices.map((executor) => (
              <Row testID={`brain-executor-${executor.id}`} accessibilityLabel={`Set Brain host to ${brainExecutorLabel(executor)}`}
                title={brainExecutorLabel(executor)} subtitle={brainExecutorMeta(executor, source.snapshot()?.executor_states?.[executor.id])}
                leading={<AgentIcon kind={agentKindFromProvider(executor.provider)} size={24} />}
                value={source.hostSwitch.switching() === executor.id ? "Switching…" : undefined}
                check={!source.hostSwitch.switching() && executor.id === hostId()}
                disabled={Boolean(source.hostSwitch.switching())} onPress={() => choose(executor)} />
            ))}
          </View>
        </View>
      </Scroll>
      {choices().moreToInstall ? <Text testID="brain-executor-install" class="px-4 text-sub text-muted-foreground">Install more with mewla deps</Text> : null}
      {source.hostSwitch.error() ? <Text testID="brain-executor-error" class="px-4 text-sub text-foreground">{source.hostSwitch.error()}</Text> : null}
    </View>
  );
}

/** "Brain host" as a sheet of its own (the can't-start notice's Switch executor); Brain's menu shows the panel as its page. */
export function BrainHostSheet(props: { open: () => boolean; onClose: () => void; source?: BrainSource }) {
  const source = props.source ?? currentBrain;
  const close = () => {
    source.hostSwitch.clearError();
    props.onClose();
  };
  // The new host is up: the sheet has done its job.
  let seen = untracked(source.hostSwitch.switched);
  effect(() => {
    const count = source.hostSwitch.switched();
    if (count === seen) return;
    seen = count;
    untracked(() => props.open() && close());
  });
  return (
    <Sheet testID="brain-executor-sheet" open={props.open} onClose={close} title="Brain host">
      <BrainHostPanel source={source} onClose={close} />
    </Sheet>
  );
}
