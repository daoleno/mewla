import { Image, Text, View } from "nsh";
import type { ImageAsset } from "nsh";
import { Button } from "nsh/base";

/** A small exclusive choice (SPEC §11): a surface track, the chosen segment canvas with ink text; taller in a sheet. */
export function Segmented<K extends string>(props: {
  /** `mark`: a logo before the label (an agent's). */
  options: ReadonlyArray<{ key: K; label: string; mark?: ImageAsset }>;
  value: K;
  onChange: (key: K) => void;
  accessibilityLabel?: string;
  testID?: string;
  /** In a sheet: 40 high, 15 pt labels. */
  inSheet?: boolean;
}) {
  const height = () => (props.inSheet ? "h-[40px]" : "h-[34px]");
  const label = () => (props.inSheet ? "text-sub font-semibold" : "text-caption font-semibold");
  return (
    <View testID={props.testID} role="tablist" accessibilityLabel={props.accessibilityLabel} class="flex-row rounded-xl bg-card p-[3px]">
      {props.options.map((option) => (
        <Button
          testID={props.testID ? `${props.testID}-${option.key}` : undefined}
          role="tab"
          selected={props.value === option.key}
          onPress={() => props.onChange(option.key)}
          class={`flex-1 flex-row items-center justify-center gap-1.5 rounded-[9px] ${height()} ${props.value === option.key ? "bg-background" : "active:opacity-70"}`}
        >
          {option.mark ? <Image src={option.mark} style={{ width: 18, height: 18 }} /> : null}
          <Text class={`${label()} ${props.value === option.key ? "text-foreground" : "text-faint"}`}>{option.label}</Text>
        </Button>
      ))}
    </View>
  );
}
