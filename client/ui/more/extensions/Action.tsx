import { Text } from "nsh";
import { Button } from "nsh/base";
import { Icon, type IconName } from "../../kit/Icon";
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
      <If when={props.busy}><Spinner size={14} tone={props.filled ? "canvas" : "soft"} /></If>
      <Text class={`text-body font-semibold ${props.filled ? "text-primary-foreground" : props.destructive ? "text-destructive" : ""}`}>{props.label}</Text>
    </Button>
  );
}

/** A small ghost button (SPEC §10.4 "an action you can take": 36 high, 15/600), with its glyph when it has one. */
export function GhostAction(props: { label: string; onPress: () => void; icon?: IconName; busy?: boolean; disabled?: boolean; testID?: string; accessibilityLabel?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel ?? props.label}
      disabled={props.disabled || props.busy}
      onPress={props.onPress}
      hitSlop={4}
      class="h-9 flex-row items-center justify-center gap-1.5 self-start rounded-xl bg-muted px-3.5 active:opacity-70 disabled:opacity-40"
    >
      <If when={props.busy} else={<If when={props.icon}>{(icon) => <Icon name={icon()} size={16} tone="ink" />}</If>}><Spinner size={14} /></If>
      <Text class="text-sub font-semibold">{props.label}</Text>
    </Button>
  );
}

/** A row's one fix, filled so it reads as the thing to tap (SPEC §15: the fix as a small button on the row). */
export function FixButton(props: { label: string; onPress: () => void; disabled?: boolean; testID?: string; accessibilityLabel?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.accessibilityLabel ?? props.label}
      disabled={props.disabled}
      onPress={props.onPress}
      hitSlop={4}
      class="h-9 items-center justify-center rounded-xl bg-primary px-3.5 active:opacity-70 disabled:opacity-40"
    >
      <Text class="text-sub font-semibold text-primary-foreground">{props.label}</Text>
    </Button>
  );
}

/** An icon-only control (close, paste): 44 round, its name for screen readers. */
export function IconAction(props: { icon: IconName; label: string; onPress: () => void; disabled?: boolean; testID?: string }) {
  return (
    <Button
      testID={props.testID}
      accessibilityLabel={props.label}
      disabled={props.disabled}
      onPress={() => props.onPress()}
      class="-my-2 size-11 items-center justify-center rounded-full hover:bg-accent active:bg-accent disabled:opacity-40"
    >
      <Icon name={props.icon} size={20} tone="soft" />
    </Button>
  );
}
