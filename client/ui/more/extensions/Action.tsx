import { Text } from "nsh";
import { Button } from "nsh/base";
import { Spinner } from "../../kit/kit";

/** A full-width button in a card: ink when it is the next step, quiet otherwise. */
export function Action(props: { label: string; onPress: () => void; filled?: boolean; busy?: boolean; disabled?: boolean; destructive?: boolean; testID?: string; accessibilityLabel?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel}
      disabled={props.disabled || props.busy}
      onPress={props.onPress}
      class={`h-[52px] flex-row items-center justify-center gap-2 rounded-2xl px-5 active:opacity-70 disabled:opacity-40 ${props.filled ? "bg-primary" : "bg-card"}`}
    >
      {props.busy ? <Spinner size={14} tone={props.filled ? "canvas" : "soft"} /> : null}
      <Text class={`text-body font-semibold ${props.filled ? "text-primary-foreground" : props.destructive ? "text-destructive" : ""}`}>{props.label}</Text>
    </Button>
  );
}
