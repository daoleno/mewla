import { Image, Text, View } from "nsh";
import type { ImageAsset } from "nsh";
import { Button } from "nsh/base";
import { Icon, type IconName } from "../icons/Icon";

/** A small exclusive choice (SPEC §11): a surface track, the chosen segment canvas with ink text; taller in a sheet. */
export function Segmented<K extends string>(props: {
  /** `mark`: a logo before the label (an agent's). `icon`: the segment is that icon alone, its label read out. */
  options: ReadonlyArray<{ key: K; label: string; mark?: ImageAsset; icon?: IconName }>;
  value: K;
  onChange: (key: K) => void;
  accessibilityLabel?: string;
  testID?: string;
  /** In a sheet: 40 high, 15 pt labels. */
  inSheet?: boolean;
  /** Each segment as wide as its label (12 at the sides, 44 at least), not a share of the row: in a header. */
  fit?: boolean;
}) {
  const height = () => (props.inSheet ? "h-[40px]" : "h-[34px]");
  const label = () => (props.inSheet ? "text-sub font-semibold" : "text-caption font-semibold");
  return (
    <View testID={props.testID} role="tablist" accessibilityLabel={props.accessibilityLabel} class="flex-row rounded-xl bg-card p-[3px]">
      {props.options.map((option) => (
        <Button
          testID={props.testID ? `${props.testID}-${option.key}` : undefined}
          role="tab"
          accessibilityLabel={option.icon ? option.label : undefined}
          selected={props.value === option.key}
          onPress={() => props.onChange(option.key)}
          class={`${option.icon ? "min-w-[44px] px-3" : props.fit ? "min-w-[64px] px-2" : "flex-1"} flex-row items-center justify-center gap-1.5 rounded-[9px] ${height()} ${props.value === option.key ? "bg-background" : "active:opacity-70"}`}
        >
          {option.mark ? <Image src={option.mark} style={{ width: 18, height: 18 }} /> : null}
          {option.icon ? (
            <Icon name={option.icon} size={18} color={props.value === option.key ? "foreground" : "faint"} />
          ) : (
            // In fit mode the word fills its segment (64 at least), not its own measure: a release build measured
            // "Today" a hair short and clipped it to "Toda" (fidelity round 2, Calendar at 360 dp).
            <Text class={`${label()} ${props.fit ? "flex-1 text-center" : ""} ${props.value === option.key ? "text-foreground" : "text-faint"}`}
              style={props.fit ? { numberOfLines: 1 } : undefined}>{option.label}</Text>
          )}
        </Button>
      ))}
    </View>
  );
}
