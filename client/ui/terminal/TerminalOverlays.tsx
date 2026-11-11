import { cleanup, Text, View } from "nsh";
import { Button, Input } from "nsh/base";
import { searchCount, type SearchState } from "../../core/terminal/text";
import { Icon, type IconName } from "../icons/Icon";

/** While scrolled up: search the history, the previous and next prompt, back to the live screen. */
export function TerminalJumpControls(props: { onSearch(): void; onPrompt(direction: -1 | 1): void; onBottom(): void }) {
  const items: Array<{ id: string; label: string; icon: IconName; press: () => void }> = [
    { id: "search", label: "Search history", icon: "search", press: props.onSearch },
    { id: "previous", label: "Previous prompt", icon: "chevron-up", press: () => props.onPrompt(-1) },
    { id: "next", label: "Next prompt", icon: "chevron-down", press: () => props.onPrompt(1) },
    { id: "bottom", label: "Scroll to bottom", icon: "arrow-down", press: props.onBottom },
  ];
  return (
    <View class="absolute bottom-4 right-4 gap-2.5">
      {items.map((item) => (
        <Button testID={`terminal-jump-${item.id}`} accessibilityLabel={item.label} onPress={item.press}
          class="h-11 w-11 items-center justify-center rounded-full border border-border bg-card active:bg-accent">
          <Icon name={item.icon} size={16} color="foreground" />
        </Button>
      ))}
    </View>
  );
}

const QUERY_DELAY_MS = 150;

/** Search over the history of the Session on screen. Older matches are the up arrow, newer ones the down arrow. */
export function TerminalSearchBar(props: { state: () => SearchState; onQuery(query: string): void; onStep(step: -1 | 1): void; onClose(): void }) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  cleanup(() => clearTimeout(timer));
  const change = (value: string) => {
    clearTimeout(timer);
    timer = setTimeout(() => props.onQuery(value), QUERY_DELAY_MS);
  };
  const button = (id: string, label: string, icon: IconName, press: () => void, disabled: () => boolean) => (
    <Button testID={`terminal-search-${id}`} accessibilityLabel={label} disabled={disabled()} onPress={press}
      class="h-9 w-9 items-center justify-center rounded-full active:bg-accent">
      <Icon name={icon} size={18} color={disabled() ? "muted-foreground" : "foreground"} />
    </Button>
  );
  return (
    <View class="absolute left-2 right-2 top-2 min-h-11 flex-row items-center gap-1 rounded-full border border-border bg-card pl-3.5 pr-1">
      <Icon name="search" size={16} />
      <Input testID="terminal-search-input" accessibilityLabel="Search the terminal history" autoFocus autoCapitalize="none" autoCorrect={false}
        returnKeyType="search" submitBehavior="submit" onChange={({ value }) => change(value)} onSubmit={() => props.onStep(-1)}
        class="flex-1 px-1.5 py-1.5 text-base" />
      <Text testID="terminal-search-count" class="min-w-9 text-right text-xs text-muted-foreground">{searchCount(props.state())}</Text>
      {button("older", "Older match", "chevron-up", () => props.onStep(-1), () => !props.state().hits.length)}
      {button("newer", "Newer match", "chevron-down", () => props.onStep(1), () => !props.state().hits.length)}
      {button("close", "Close search", "close", props.onClose, () => false)}
    </View>
  );
}

/** The native VT did not start: what went wrong, and Retry. */
export function TerminalRendererFailure(props: { message: string; onRetry(): void }) {
  return (
    <View testID="terminal-renderer-failure" class="absolute inset-0 items-center justify-center bg-background px-7">
      <Text class="text-center text-[15px] font-semibold">Terminal renderer could not start</Text>
      <Text class="mt-2 text-center text-xs text-muted-foreground">{props.message || "The embedded terminal did not become ready."}</Text>
      <Button testID="terminal-renderer-retry" accessibilityLabel="Retry Terminal renderer" onPress={props.onRetry}
        class="mt-4 min-h-10 min-w-24 items-center justify-center rounded-full border border-border bg-card px-4 active:bg-accent">
        <Text class="text-[13px] font-semibold">Retry</Text>
      </Button>
    </View>
  );
}
