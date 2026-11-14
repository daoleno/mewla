import { Text, View } from "nsh";
import { Button } from "nsh/base";
import type { CalendarStatus } from "../../core/calendar/model";

/** A grouped list's section title, as iOS Settings and Android preferences show them. */
export function SectionTitle(props: { title: string }) {
  return <View class="px-4 pb-1.5 pt-5"><Text class="text-sm font-semibold text-muted-foreground">{props.title}</Text></View>;
}

/** A plain row of a grouped list: a title, an optional detail at the end. */
export function Row(props: { title: string; detail?: string; onPress?: () => void; testID?: string; tone?: "destructive" | "primary"; disabled?: boolean }) {
  const tone = () => (props.disabled ? "text-muted-foreground" : props.tone === "destructive" ? "text-destructive" : props.tone === "primary" ? "font-semibold" : "");
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled || !props.onPress}
      class="flex-row items-center justify-between gap-3 border-b border-border bg-card px-4 py-3.5 active:bg-accent">
      <Text class={`flex-1 text-base ${tone()}`}>{props.title}</Text>
      <If when={props.detail}>{(detail) => <Text class="text-base text-muted-foreground">{detail()}</Text>}</If>
    </Button>
  );
}

/**
 * The status dot (SPEC §10.4): the seal for what waits on you, `ok` for done, `warning` for a failure, ink while
 * it runs, a ring for the rest.
 */
export function StatusDot(props: { status: CalendarStatus }) {
  const color = () => {
    switch (props.status) {
      case "waiting": return "bg-seal";
      case "running": return "bg-ring";
      case "failed": return "bg-warning";
      case "completed": return "bg-ok";
      default: return "border border-muted-foreground";
    }
  };
  return <View class={`size-2 rounded-full ${color()}`} />;
}

/** A row of choices, one selected: the platform's segmented control look. */
export function Choices<T extends string>(props: { value: T; options: { value: T; label: string }[]; onChange: (value: T) => void; testID?: string }) {
  return (
    <View testID={props.testID} class="flex-row flex-wrap gap-2">
      {props.options.map((option) => (
        // Chosen or not, a chip has the same border and the same weight, so choosing one never changes
        // its width (a bolder "Weekly" wrapped to "Weekl / y" and doubled the row's height, as v0.3.1's
        // ChipGroup never did: its states differ in colour only).
        <Button testID={`${props.testID}-${option.value}`} onPress={() => props.onChange(option.value)}
          class={props.value === option.value ? "rounded-full border border-primary bg-primary px-3.5 py-1.5" : "rounded-full border border-border px-3.5 py-1.5 active:bg-accent"}>
          <Text class={props.value === option.value ? "line-clamp-1 text-sm font-medium text-primary-foreground" : "line-clamp-1 text-sm font-medium"}>{option.label}</Text>
        </Button>
      ))}
    </View>
  );
}
