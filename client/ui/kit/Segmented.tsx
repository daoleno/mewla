import { Image, signal, Text, untracked, View } from "nsh";
import type { ImageAsset } from "nsh";
import { Button } from "nsh/base";
import { Icon, type IconName } from "../icons/Icon";
import { indicatorStops } from "../../core/shell/tabPager";
import { color } from "./colors";
import type { TabPager } from "./TabPager";

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
  /**
   * The tabs of a row of pages (`kit/TabPager.tsx`): the chosen segment's canvas is one shape that follows the
   * pages under the finger, and the labels turn ink as it reaches them. Its keys are the options' keys, in order.
   */
  pager?: TabPager<K>;
}) {
  const height = () => (props.inSheet ? "h-[40px]" : "h-[34px]");
  const label = () => (props.inSheet ? "text-sub font-semibold" : "text-caption font-semibold");
  const pager = untracked(() => props.pager);
  // The segments as laid out in the track, for the moving shape.
  const frames = signal<Array<{ x: number; width: number } | undefined>>([]);
  const measured = () => {
    const f = frames();
    // Every segment, not `every` (which skips the holes of segments not laid out yet).
    const all = Array.from({ length: props.options.length }, (_, i) => f[i]);
    return all.every((frame) => frame !== undefined && frame.width > 0) ? (all as Array<{ x: number; width: number }>) : null;
  };
  // The moving shape stands in for the chosen segment's own fill once the pages and the segments are measured.
  const tracking = () => Boolean(pager && pager.progress() && measured());
  const marker = () => {
    const progress = pager?.progress();
    const segments = measured();
    if (!progress || !segments) return null;
    const base = segments[0]!.width;
    const stops = indicatorStops(segments, base);
    const input = segments.map((_, i) => i);
    return {
      base,
      transform: [
        { translateX: progress.interpolate({ inputRange: input, outputRange: stops.translate }) },
        { scaleX: progress.interpolate({ inputRange: input, outputRange: stops.scale }) },
      ],
    };
  };
  // A label turns ink as the shape reaches it.
  const labelColor = (i: number) => {
    const progress = pager?.progress();
    if (!progress || !tracking()) return undefined;
    return progress.interpolate({ inputRange: [i - 1, i, i + 1], outputRange: [color("faint"), color("ink"), color("faint")] });
  };
  // Sized to its word in fit mode: one line, clipped, never wrapped or ended in "…".
  const fitStyle = { numberOfLines: 1, textOverflow: "clip" } as const;
  const labelStyle = (i: number) => (pager ? { ...(props.fit ? fitStyle : {}), color: labelColor(i) } : props.fit ? fitStyle : undefined);
  return (
    <View testID={props.testID} role="tablist" accessibilityLabel={props.accessibilityLabel} class="flex-row rounded-xl bg-card p-[3px]">
      <If when={tracking()}><View testID={props.testID ? `${props.testID}-marker` : undefined} class={`absolute left-0 top-[3px] rounded-[9px] bg-background ${height()}`}
          style={{ width: marker()?.base ?? 0, transform: marker()?.transform ?? [] }} /></If>
      {props.options.map((option, i) => (
        <Button
          testID={props.testID ? `${props.testID}-${option.key}` : undefined}
          role="tab"
          accessibilityLabel={option.icon ? option.label : undefined}
          selected={props.value === option.key}
          onPress={() => (pager ? pager.choose(option.key) : props.onChange(option.key))}
          onLayout={pager ? (e) => {
            const next = [...untracked(frames)];
            next.length = props.options.length;
            next[i] = { x: e.bounds.origin.x, width: e.bounds.size.width };
            frames.set(next);
          } : undefined}
          class={`${option.icon || props.fit ? "min-w-[44px] px-3" : "flex-1"} flex-row items-center justify-center gap-1.5 rounded-[9px] ${height()} ${props.value === option.key && !tracking() ? "bg-background" : "active:opacity-70"}`}
        >
          {/* An agent's logo, 16 pt, at half strength while its segment is not chosen (SPEC §10.4). */}
          <If when={option.mark}>{(mark) => <Image src={mark()} style={{ width: 16, height: 16, opacity: props.value === option.key ? 1 : 0.5 }} />}</If>
          <If when={option.icon} else={<Text class={`${label()} ${props.value === option.key ? "text-foreground" : "text-faint"}`} style={labelStyle(i)}>{option.label}</Text>}>{(icon) => <Icon name={icon()} size={18} color={props.value === option.key ? "foreground" : "faint"} />}</If>
        </Button>
      ))}
    </View>
  );
}
