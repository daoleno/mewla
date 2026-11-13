import { Scroll, Text, View } from "nsh";
import { Button } from "nsh/base";
import { AgentIcon } from "../icons/AgentIcon";
import { agentKindFromProvider } from "../../core/sessions/workerPresentation";
import type { BrainExecutorRef } from "../../core/brain";
import { brainExecutorLabel } from "../../core/brainMenu";
import { activeExecutorMentionAtEnd, executorMentionChoices, insertExecutorMention } from "../../core/brainMention";

/** An `@word` is being typed and some agent matches it: the list shows, and Return waits for a pick. */
export function mentionPanelOpen(text: string, executors: readonly BrainExecutorRef[]): boolean {
  const mention = activeExecutorMentionAtEnd(text);
  return Boolean(mention && executorMentionChoices(executors, mention.query).length);
}

/**
 * Typing `@` in Brain's composer (soft-final/SPEC.md §1.4): the installed agents
 * that match the `@word` (logo, name, `@id`), in a floating panel of 48 pt rows;
 * a pick hands back the draft with `@<id> ` in place of the `@word`. It closes when
 * the token stops matching. The host places it 4 pt above the field.
 */
export function MentionPanel(props: {
  text: () => string;
  /** The agents to offer: the installed ones (`brainExecutorChoices`). */
  executors: () => readonly BrainExecutorRef[];
  onPick: (draft: string) => void;
}) {
  const mention = () => activeExecutorMentionAtEnd(props.text());
  const choices = () => (mention() ? executorMentionChoices(props.executors(), mention()!.query) : []);
  return (
    <View>
      <If when={choices().length}><View testID="brain-mention-panel" class="overflow-hidden rounded-(--radius-group) bg-sheet-surface shadow-lg">
          <Scroll keyboardShouldPersistTaps="always" style={{ maxHeight: 240 }}>
            {choices().map((executor, index) => (
              <Button testID={`brain-mention-${executor.id}`} accessibilityLabel={`Mention ${brainExecutorLabel(executor)}`}
                onPress={() => {
                  const active = activeExecutorMentionAtEnd(props.text());
                  if (active) props.onPick(insertExecutorMention(props.text(), active, executor.id));
                }}
                class={`h-[48px] flex-row items-center gap-3 px-4 active:opacity-70 ${index ? "border-t border-border" : ""}`}>
                <AgentIcon kind={agentKindFromProvider(executor.provider)} size={22} />
                <Text class="text-body text-foreground line-clamp-1">{brainExecutorLabel(executor)}</Text>
                <Text class="flex-1 text-sub text-muted-foreground line-clamp-1">@{executor.id}</Text>
              </Button>
            ))}
          </Scroll>
        </View></If>
    </View>
  );
}
