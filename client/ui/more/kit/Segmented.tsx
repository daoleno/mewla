import { Image, Text, View } from "nsh";
import type { ImageAsset } from "nsh";
import { Button } from "nsh/base";

/** A small exclusive choice: the platform's segmented control look, ink on a tint well. */
export function Segmented<K extends string>(props: {
  /** `mark`: a logo before the label (an agent's). */
  options: ReadonlyArray<{ key: K; label: string; mark?: ImageAsset }>;
  value: K;
  onChange: (key: K) => void;
  accessibilityLabel?: string;
  testID?: string;
}) {
  return (
    <View testID={props.testID} role="tablist" accessibilityLabel={props.accessibilityLabel} class="flex-row rounded-xl bg-secondary p-0.5">
      {props.options.map((option) => (
        <Button
          testID={props.testID ? `${props.testID}-${option.key}` : undefined}
          role="tab"
          selected={props.value === option.key}
          onPress={() => props.onChange(option.key)}
          class={props.value === option.key ? "flex-1 flex-row items-center justify-center gap-1.5 rounded-[10px] bg-card py-1.5 shadow-sm" : "flex-1 flex-row items-center justify-center gap-1.5 rounded-[10px] py-1.5 active:opacity-60"}
        >
          {option.mark ? <Image src={option.mark} style={{ width: 18, height: 18 }} /> : null}
          <Text class={props.value === option.key ? "text-sm font-semibold" : "text-sm text-muted-foreground"}>{option.label}</Text>
        </Button>
      ))}
    </View>
  );
}
