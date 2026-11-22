import { Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button } from "nsh/base";
import { rowDim, rowLine, type AgendaRow } from "../../core/calendar/agenda";
import { Icon } from "../kit/Icon";
import type { IconName } from "../kit/Icon";
import { dense } from "../kit/density";
import { Spinner } from "../kit/wait";
import { clock } from "./clock";

/**
 * The Calendar's rows (w-cal) on every client: the phone's at its type sizes, the Mac's at the mock's. One layout.
 */
export function rowSize() {
  return dense({
    phone: { min: "min-h-[52px]", pad: "px-[14px] py-[7px]", gap: "gap-[11px]", title: "text-[15px] leading-[20px]", line: "text-[13px] leading-[18px]", time: "text-[14px]", timeWidth: 41, hair: 66, tileHair: 57 },
    window: { min: "min-h-[44px]", pad: "px-[14px] py-1.5", gap: "gap-[11px]", title: "text-[14px] leading-[19px]", line: "text-[12px] leading-[16px]", time: "text-[13px]", timeWidth: 44, hair: 69, tileHair: 57 },
  });
}

/** The time column's width: "10:00" fits the size's own, "10:00 PM" more. */
function timeWidth(sample: string): number {
  return /[A-Za-z]/.test(sample) ? rowSize().timeWidth + 20 : rowSize().timeWidth;
}

/** The hairline over every row but a card's first, from the text's start (w-cal's `.lrow + .lrow`). */
export function TopHairline(props: { first: () => boolean; left: number }) {
  return <If when={!props.first()}><View class="absolute right-0 top-0 h-px bg-border" style={{ left: props.left }} /></If>;
}

/** The seal's dot: something only the person can move on. */
export function SealDot(props: { testID?: string; size?: number }) {
  const size = props.size ?? 8;
  return <View testID={props.testID} class="rounded-full bg-seal" style={{ width: size, height: size }} />;
}

function Trailing(props: { row: () => AgendaRow }) {
  return (
    <View class="w-[18px] items-center">
      {(() => {
        switch (props.row().state) {
          case "done": return <Icon name="check-circle" size={18} tone="ok" />;
          case "failed": return <Icon name="alert-circle" size={18} tone="warning" />;
          case "needs_you": return <SealDot testID="calendar-item-needs" />;
          case "running": return <Spinner size={16} label="Running" />;
          case "past": return null;
          default: return <Icon name="chevron-right" size={16} tone="faint" />;
        }
      })()}
    </View>
  );
}

/**
 * One agenda row: the time on this device's clock, the title over one quiet line, then how it went. Done and passed
 * rows dim; a failed run's line is amber, a run that needs the person ends with the seal.
 */
export function AgendaRowView(props: { row: () => AgendaRow; first: () => boolean; date?: boolean; onPress: () => void }) {
  const size = rowSize();
  const zone = () => clock().zones.device;
  const time = () => clock().format.time(props.row().at, zone());
  const line = () => rowLine(props.row(), clock(), { date: props.date });
  const width = () => timeWidth(clock().format.time(clock().now, zone()));
  const lineTone = () => (props.row().state === "failed" ? "text-warning" : props.row().state === "needs_you" ? "text-seal-ink" : "text-muted-foreground");
  return (
    <Button testID={`calendar-item-${props.row().item.id}`} onPress={props.onPress}
      accessibilityLabel={[time(), props.row().item.title, line()].filter(Boolean).join(", ")}
      class={`${size.min} flex-row items-center ${size.gap} ${size.pad} active:bg-accent`}>
      <TopHairline first={props.first} left={width() + 25} />
      <Text class={`${size.time} text-muted-foreground`} style={{ width: width(), fontVariant: ["tabular-nums"], opacity: rowDim(props.row()) ? 0.55 : 1 }}>{time()}</Text>
      <View class="min-w-0 flex-1" style={{ opacity: rowDim(props.row()) ? 0.55 : 1 }}>
        <Text class={`${size.title} line-clamp-1`}>{props.row().item.title}</Text>
        <If when={line()}>{(text) => <Text testID="calendar-item-line" class={`${size.line} ${lineTone()} line-clamp-1`}>{text()}</Text>}</If>
      </View>
      <Trailing row={props.row} />
    </Button>
  );
}

/** The ink now line between what is past and what is next (w-cal). */
export function NowLine(props: { first: () => boolean }) {
  const now = () => clock().format.time(clock().now, clock().zones.device);
  const width = () => timeWidth(now());
  return (
    <View testID="calendar-now" accessibilityLabel={`Now, ${now()}`} class="min-h-[24px] flex-row items-center gap-[11px] px-[14px]">
      <TopHairline first={props.first} left={width() + 25} />
      <Text class={dense({ phone: "text-[13px] font-bold", window: "text-[12px] font-semibold" })} style={{ width: width(), fontVariant: ["tabular-nums"] }}>{now()}</Text>
      <View class="flex-1 flex-row items-center">
        <View class="rounded-full bg-foreground" style={{ width: 7, height: 7, marginRight: -3 }} />
        <View class="flex-1 bg-foreground" style={{ height: 1.5 }} />
      </View>
    </View>
  );
}

/** A row led by a glyph on the neutral tile (a Trigger, Earlier): a title, a line, an end, a chevron. */
export function TileRow(props: {
  testID?: string; icon: IconName; title: () => string; line: () => string; first: () => boolean; onPress?: () => void;
  /** A line of more than one tone, in place of `line`. */
  lineNode?: JSX.Element;
  seal?: () => boolean; end?: () => string | undefined; lineTone?: () => string; chevron?: "right" | "down" | "up"; accessibilityLabel?: () => string;
}) {
  const size = rowSize();
  return (
    <Button testID={props.testID} onPress={props.onPress} disabled={!props.onPress} accessibilityLabel={props.accessibilityLabel?.()}
      class={`${size.min} flex-row items-center ${size.gap} ${size.pad} active:bg-accent`}>
      <TopHairline first={props.first} left={size.tileHair} />
      <View class="size-8 items-center justify-center rounded-[10px] bg-tile">
        <Icon name={props.icon} size={18} tone="ink" />
        <If when={props.seal?.()}><View class="absolute -right-0.5 -top-0.5 rounded-full border-2 border-card"><SealDot testID={props.testID ? `${props.testID}-seal` : undefined} /></View></If>
      </View>
      <View class="min-w-0 flex-1">
        <Text class={`${size.title} line-clamp-1`}>{props.title()}</Text>
        <If when={props.lineNode} else={<Text class={`${size.line} ${props.lineTone?.() ?? "text-muted-foreground"} line-clamp-1`}>{props.line()}</Text>}>{props.lineNode}</If>
      </View>
      <If when={props.end?.()}>{(end) => <Text class={`${size.time} text-muted-foreground`} style={{ fontVariant: ["tabular-nums"] }}>{end()}</Text>}</If>
      <If when={props.onPress}><Icon name={props.chevron === "down" ? "chevron-down" : props.chevron === "up" ? "chevron-up" : "chevron-right"} size={16} tone="faint" /></If>
    </Button>
  );
}
