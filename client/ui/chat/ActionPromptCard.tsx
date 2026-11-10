import { signal, Text, View } from "nsh";
import { Button, TextView } from "nsh/base";
import type { TerminalActionPrompt, TerminalActionPromptOption } from "../../core/session/terminalActionPrompt";
import { Icon } from "../icons/Icon";

/**
 * "Waiting for confirmation": what the agent asks (secrets already
 * redacted), and its options as rows; the default one is marked. A tap answers
 * (the agent's own approval, or the terminal menu's number). Keyed by the
 * prompt's id: a new prompt starts clean.
 */
export function ActionPromptCard(props: { prompt: TerminalActionPrompt; connected: () => boolean; answer: (option: TerminalActionPromptOption) => Promise<void> }) {
  const sending = signal("");
  const failed = signal(false);
  const choose = async (option: TerminalActionPromptOption) => {
    if (sending()) return;
    sending.set(option.id);
    failed.set(false);
    try {
      await props.answer(option);
    } catch {
      failed.set(true);
    } finally {
      sending.set("");
    }
  };
  return (
    <View testID="action-prompt" class="mb-2 gap-2 rounded-2xl border border-border bg-card px-3 py-3">
      <View class="flex-row items-center gap-2">
        <Icon name="hand" size={16} color="seal" />
        <Text class="flex-1 text-sm font-semibold">{props.prompt.title || "Waiting for confirmation"}</Text>
      </View>
      {props.prompt.requestLabel ? <Text class="text-xs font-semibold uppercase text-muted-foreground">{props.prompt.requestLabel}</Text> : null}
      {props.prompt.requestText ? (
        <View class="max-h-48 overflow-hidden rounded-lg bg-secondary px-2.5 py-2">
          <TextView document={{ blocks: [{ type: "codeBlock", code: props.prompt.requestText, language: "" }] }} selectable />
        </View>
      ) : null}
      <View class="gap-1.5">
        {props.prompt.options.map((option) => (
          <Button testID={`prompt-option-${option.id}`} onPress={() => void choose(option)} disabled={Boolean(sending()) || !props.connected()}
            class={option.primary ? "flex-row items-center gap-2 rounded-xl bg-primary px-3 py-2.5 active:opacity-80" : "flex-row items-center gap-2 rounded-xl border border-border px-3 py-2.5 active:bg-accent"}>
            <View class="flex-1 gap-0.5">
              <Text class={option.primary ? "text-base font-semibold text-primary-foreground" : option.destructive ? "text-base text-destructive" : "text-base"}>
                {sending() === option.id ? "Sending…" : option.label}
              </Text>
              {option.description ? <Text class={option.primary ? "text-xs text-primary-foreground" : "text-xs text-muted-foreground"}>{option.description}</Text> : null}
            </View>
            {option.id === props.prompt.defaultOptionId ? (
              <Text class={option.primary ? "rounded-full border border-primary-foreground px-2 py-0.5 text-xs text-primary-foreground" : "rounded-full border border-border px-2 py-0.5 text-xs text-muted-foreground"}>Default</Text>
            ) : null}
          </Button>
        ))}
      </View>
      {failed() ? <Text testID="prompt-failed" class="text-sm text-destructive">Could not send. Try again.</Text> : null}
      {!props.connected() ? <Text class="text-sm text-muted-foreground">Reconnecting to answer…</Text> : null}
    </View>
  );
}
