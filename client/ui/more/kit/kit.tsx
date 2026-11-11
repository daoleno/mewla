import type { AccessibilityRole, JSX, ScrollRef } from "nsh";
import { announce, Canvas, Clipboard, effect, platform, Scroll, Text, untracked, View } from "nsh";
import { AlertDialog, Button, Input, Switch } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import type { PageColumn } from "../../../core/web/places";
import { NavPage } from "../../shell/NavPage";
import { color, type ColorName } from "./colors";
import { Icon, type IconName } from "./Icon";

/**
 * The building blocks of the More pages: a page with the system header, inset
 * grouped sections of rows on a card, and the few states a page has (busy,
 * empty, failed). Paper and ink, hairlines for structure (client/DESIGN.md).
 */

export function Page(props: {
  title: string;
  testID?: string;
  headerRight?: JSX.Element;
  refreshing?: boolean;
  onRefresh?: () => void;
  /** The widest the content grows on a phone or tablet, in points (720; a dashboard takes more). */
  maxWidth?: number;
  /** The desktop column (client/DESIGN.md): 760 by default, 1200 for Resources. */
  column?: Exclude<PageColumn, "full" | "reading">;
  /** The page's scroll view, for a page that brings something at its top into view. */
  scrollRef?: (ref: ScrollRef) => void;
  /** A panel beside the scrolling page (a wide screen's details), or nothing. */
  aside?: JSX.Element;
  children?: JSX.Element;
}) {
  // Its column sits in a full-width scroll, so the scrollbar stays at the window's edge; the header row shares it.
  const width = () => (mobile ? (props.maxWidth ?? 720) : props.column === "wide" ? 1200 : 760);
  const scroll = (
    <Scroll ref={props.scrollRef} class="flex-1 bg-background" contentClass="px-4 pb-10 pt-2" refreshing={props.refreshing} onRefresh={props.onRefresh}>
      <View class="w-full self-center" style={{ maxWidth: width() }}>{props.children}</View>
    </Scroll>
  );
  // Whether the page has a side panel is fixed when it is made; what the panel shows is not.
  const withAside = untracked(() => props.aside !== undefined);
  return (
    <NavPage title={props.title} testID={props.testID} headerRight={props.headerRight} column={props.column ?? "page"} frame={false}>
      {withAside ? <View class="flex-1 flex-row bg-background">{scroll}{props.aside}</View> : scroll}
    </NavPage>
  );
}

/** A caption, one card of rows separated by inset hairlines, an optional footnote. */
export function Section(props: { title?: string; detail?: JSX.Element; footer?: string; testID?: string; children?: JSX.Element }) {
  return (
    <View class="mt-6" testID={props.testID}>
      {props.title || props.detail ? (
        <View class="mb-1.5 flex-row items-center justify-between px-4">
          <Text class="text-sm font-medium text-muted-foreground" role="header">{props.title ?? ""}</Text>
          {props.detail ?? null}
        </View>
      ) : null}
      <View class="overflow-hidden rounded-2xl border border-border bg-card">
        <View class="-mb-px">{props.children}</View>
      </View>
      {props.footer ? <Text class="mt-1.5 px-4 text-xs text-muted-foreground">{props.footer}</Text> : null}
    </View>
  );
}

/** The hairline under a row; the section's card hides the last one. */
function Hairline(props: { inset: number }) {
  return <View class="absolute bottom-0 right-0 h-px bg-border" style={{ left: props.inset }} />;
}

export interface RowProps {
  title: string;
  subtitle?: string;
  value?: string;
  icon?: IconName;
  leading?: JSX.Element;
  trailing?: JSX.Element;
  chevron?: boolean;
  check?: boolean;
  destructive?: boolean;
  disabled?: boolean;
  mono?: boolean;
  testID?: string;
  onPress?: () => void;
  onLongPress?: () => void;
  /** What a screen reader says for the row, when its texts are not enough (its state, its account). */
  accessibilityLabel?: string;
  /** What pressing it does. */
  accessibilityHint?: string;
  /** What it is for screen readers ("button" when it can be pressed). */
  role?: AccessibilityRole;
  /** A radio or a choice that is the chosen one. */
  selected?: boolean;
  /** A row that opens and closes what is under it. */
  expanded?: boolean;
}

/** One row: a glyph, a title over a subtitle, a value or a control, a chevron when it opens something. */
export function Row(props: RowProps) {
  const body = (
    <View class="min-h-[52px] flex-row items-center gap-3 px-4 py-3">
      {props.leading ?? (props.icon ? <Icon name={props.icon} tone={props.destructive ? "seal" : "soft"} /> : null)}
      <View class="min-w-0 flex-1">
        <Text class={props.destructive ? "text-base text-destructive" : props.mono ? "font-mono text-sm" : "text-base"} style={{ numberOfLines: 2 }}>{props.title}</Text>
        {props.subtitle ? <Text class="mt-0.5 text-sm text-muted-foreground line-clamp-3">{props.subtitle}</Text> : null}
      </View>
      {props.value ? <Text class="max-w-[50%] text-right text-base text-muted-foreground line-clamp-1" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text> : null}
      {props.trailing ?? null}
      {props.check ? <Icon name="check" tone="ink" size={18} /> : null}
      {props.chevron ? <Icon name="chevron-right" size={16} tone="faint" /> : null}
      <Hairline inset={props.icon || props.leading ? 48 : 16} />
    </View>
  );
  if (!props.onPress && !props.onLongPress) {
    return (
      <View testID={props.testID} class={props.disabled ? "opacity-50" : ""} accessible={props.accessibilityLabel ? true : undefined} accessibilityLabel={props.accessibilityLabel} role={props.role}>
        {body}
      </View>
    );
  }
  // A long press, a hint or an open/closed state needs the plain pressable view (a Button takes none of them).
  if (props.onLongPress || props.accessibilityHint || props.expanded !== undefined) {
    return (
      <View
        testID={props.testID}
        role={props.role ?? "button"}
        accessible
        accessibilityLabel={props.accessibilityLabel}
        accessibilityHint={props.accessibilityHint}
        accessibilityState={{ disabled: Boolean(props.disabled), selected: props.selected, expanded: props.expanded }}
        disabled={props.disabled}
        onPress={props.onPress}
        onLongPress={props.onLongPress}
        class="active:bg-accent disabled:opacity-50"
      >
        {body}
      </View>
    );
  }
  return (
    <Button testID={props.testID} disabled={props.disabled} selected={props.selected} accessibilityLabel={props.accessibilityLabel} role={props.role} onPress={props.onPress} class="active:bg-accent disabled:opacity-50">
      {body}
    </Button>
  );
}

/** A setting that takes effect at once. `busy` while the computer has not answered yet. */
export function SwitchRow(props: { title: string; subtitle?: string; checked: boolean; busy?: boolean; disabled?: boolean; testID?: string; accessibilityLabel?: string; onChange: (checked: boolean) => void }) {
  return (
    <Row
      title={props.title}
      subtitle={props.subtitle}
      disabled={props.disabled}
      trailing={
        <View class="flex-row items-center gap-2">
          {props.busy ? <Spinner size={16} /> : null}
          <Switch
            testID={props.testID}
            accessibilityLabel={props.accessibilityLabel ?? props.title}
            checked={props.checked}
            disabled={props.disabled || props.busy}
            onChange={({ checked }) => props.onChange(checked)}
            class="disabled:opacity-50"
            trackClass="h-[30px] w-[50px] rounded-full bg-secondary border border-border checked:bg-primary checked:border-primary"
            thumbClass="m-[2px] size-[24px] rounded-full bg-card shadow-sm checked:translate-x-[20px]"
          />
        </View>
      }
    />
  );
}

/** The platform-quiet wait mark: an ink arc turning. */
export function Spinner(props: { size?: number; tone?: ColorName }) {
  const size = () => props.size ?? 20;
  return (
    <Canvas
      class="animate-spin"
      style={{ width: size(), height: size() }}
      accessibilityLabel="Loading"
      draw={(cx, s) => {
        const r = Math.min(s.width, s.height) / 2 - 1.5;
        cx.lineWidth = 2;
        cx.lineCap = "round";
        cx.strokeStyle = color(props.tone ?? "soft");
        cx.beginPath();
        cx.arc(s.width / 2, s.height / 2, r, 0, Math.PI * 1.5);
        cx.stroke();
      }}
    />
  );
}

/** What a page shows instead of its content: busy, empty or failed, with at most one way out. */
export function EmptyState(props: { title?: string; detail?: string; busy?: boolean; icon?: IconName; tone?: ColorName; action?: string; onAction?: () => void; testID?: string }) {
  return (
    <View testID={props.testID} class="items-center gap-2 px-6 py-16">
      {props.busy ? <Spinner size={24} /> : props.icon ? <Icon name={props.icon} size={28} tone={props.tone} /> : null}
      {props.title ? <Text class="mt-1 text-center text-lg font-semibold">{props.title}</Text> : null}
      {props.detail ? <Text class="text-center text-sm text-muted-foreground">{props.detail}</Text> : null}
      {props.action && props.onAction ? <PillButton testID={props.testID ? `${props.testID}-action` : undefined} title={props.action} onPress={props.onAction} /> : null}
    </View>
  );
}

/** A capsule button: ink when it is the page's one action, tint otherwise. */
export function PillButton(props: { title: string; onPress: () => void; filled?: boolean; busy?: boolean; disabled?: boolean; destructive?: boolean; testID?: string }) {
  const tone = props.filled ? "bg-primary" : "bg-secondary";
  return (
    <Button testID={props.testID} disabled={props.disabled || props.busy} onPress={props.onPress} class={`mt-2 flex-row items-center gap-2 rounded-full px-4 py-2 active:opacity-70 disabled:opacity-50 ${tone}`}>
      {props.busy ? <Spinner size={14} tone={props.filled ? "paper" : "soft"} /> : null}
      <Text class={props.filled ? "text-sm font-medium text-primary-foreground" : props.destructive ? "text-sm font-medium text-destructive" : "text-sm font-medium"}>{props.title}</Text>
    </Button>
  );
}

const NOTICE_GLYPH: Record<"warning" | "failed" | "success" | "info", { icon: IconName; tone: ColorName }> = {
  warning: { icon: "alert", tone: "warning" },
  failed: { icon: "alert", tone: "failed" },
  success: { icon: "check-circle", tone: "ready" },
  info: { icon: "info", tone: "soft" },
};

/**
 * A failure next to what failed, with nothing else to decide but trying again;
 * also a quiet word that something went through (`success`) or wants a look
 * (`info`), with its own glyph. `busy` while its action runs; `live` announces it.
 */
export function Notice(props: {
  title: string;
  detail?: string;
  action?: string;
  onAction?: () => void;
  tone?: "warning" | "failed" | "success" | "info";
  icon?: IconName;
  busy?: boolean;
  actionDisabled?: boolean;
  live?: boolean;
  testID?: string;
}) {
  const glyph = () => NOTICE_GLYPH[props.tone ?? "warning"];
  // Android hears a live region; VoiceOver is told.
  if (platform.os === "ios") {
    effect(() => {
      if (props.live) announce(props.title);
    });
  }
  return (
    <View testID={props.testID} android={{ accessibilityLiveRegion: props.live ? "polite" : "none" }} class="mt-4 flex-row items-start gap-3 rounded-2xl border border-border bg-card px-4 py-3">
      {props.busy ? <Spinner size={18} /> : <Icon name={props.icon ?? glyph().icon} size={18} tone={glyph().tone} />}
      <View class="flex-1">
        <Text class="text-sm font-medium">{props.title}</Text>
        {props.detail ? <Text class="mt-0.5 text-sm text-muted-foreground">{props.detail}</Text> : null}
      </View>
      {props.action && props.onAction ? (
        <Button disabled={props.actionDisabled || props.busy} onPress={props.onAction} class="active:opacity-60 disabled:opacity-40"><Text class="text-sm font-semibold">{props.action}</Text></Button>
      ) : null}
    </View>
  );
}

export type Status = "ready" | "running" | "warning" | "failed" | "blocked" | "neutral" | "needs";

/** A state's glyph and word: the six Work states survive greyscale (client/DESIGN.md). */
export function StatusMark(props: { status: Status; label?: string }) {
  const tone = (): ColorName => (props.status === "neutral" ? "soft" : props.status === "needs" ? "seal" : props.status);
  return (
    <View class="flex-row items-center gap-1.5">
      <Canvas
        style={{ width: 13, height: 13 }}
        accessibilityElementsHidden
        draw={(cx) => {
          const ink = color(tone());
          cx.strokeStyle = ink;
          cx.fillStyle = ink;
          cx.lineWidth = 1.5;
          cx.beginPath();
          if (props.status === "ready") {
            cx.arc(6.5, 6.5, 6, 0, Math.PI * 2);
            cx.fill();
            cx.strokeStyle = color("card");
            cx.beginPath();
            cx.moveTo(3.8, 6.7);
            cx.lineTo(5.7, 8.5);
            cx.lineTo(9.2, 4.6);
            cx.stroke();
          } else if (props.status === "warning") {
            cx.moveTo(6.5, 1.2);
            cx.lineTo(12, 11.5);
            cx.lineTo(1, 11.5);
            cx.closePath();
            cx.stroke();
          } else if (props.status === "failed") {
            cx.rect(1.2, 1.2, 10.6, 10.6);
            cx.moveTo(4, 4);
            cx.lineTo(9, 9);
            cx.moveTo(9, 4);
            cx.lineTo(4, 9);
            cx.stroke();
          } else if (props.status === "blocked" || props.status === "neutral") {
            cx.setLineDash([2, 2]);
            cx.arc(6.5, 6.5, 5.5, 0, Math.PI * 2);
            cx.stroke();
          } else if (props.status === "running") {
            cx.arc(6.5, 6.5, 5.5, 0, Math.PI * 1.4);
            cx.stroke();
          } else {
            cx.arc(6.5, 6.5, 5, 0, Math.PI * 2);
            cx.fill();
          }
        }}
      />
      {props.label ? <Text class="text-sm text-muted-foreground">{props.label}</Text> : null}
    </View>
  );
}

/** A plain paper tag. */
export function Tag(props: { label: string }) {
  return (
    <View class="rounded-full border border-border px-2 py-0.5">
      <Text class="text-xs font-medium text-muted-foreground">{props.label}</Text>
    </View>
  );
}

/** "Show N more items" / "Show fewer items" under a long card. */
export function ShowMore(props: { hidden: number; expanded: boolean; onToggle: () => void; testID?: string }) {
  return (
    <Button testID={props.testID} onPress={props.onToggle} class="px-4 py-3 active:bg-accent">
      <Text class="text-sm font-medium text-muted-foreground">{props.expanded ? "Show fewer items" : `Show ${props.hidden} more ${props.hidden === 1 ? "item" : "items"}`}</Text>
    </Button>
  );
}

/** A thin bar: `share` of the width filled in ink, on the tint. */
export function Meter(props: { share: number; tone?: ColorName; height?: number }) {
  return (
    <View class="overflow-hidden rounded-full bg-secondary" style={{ height: props.height ?? 3 }}>
      <View class="h-full rounded-full" style={{ width: `${Math.round(Math.max(0, Math.min(1, props.share)) * 1000) / 10}%`, backgroundColor: color(props.tone ?? "ink") }} />
    </View>
  );
}

/** A framed one-line field (or several lines) on the card, with what it is for above it. */
export function Field(props: {
  label?: string;
  value: string;
  placeholder?: string;
  masked?: boolean;
  mono?: boolean;
  multiline?: boolean;
  keyboardType?: "default" | "url" | "email-address" | "number-pad";
  testID?: string;
  /** What a screen reader says when there is no visible label. */
  accessibilityLabel?: string;
  onChange: (value: string) => void;
  onSubmit?: () => void;
  /** The field lost focus: commit what was typed. */
  onCommit?: () => void;
  trailing?: JSX.Element;
  /** Takes the focus (and the keyboard) when it appears. */
  autoFocus?: boolean;
  /** Read only for now (while what it holds is being sent). */
  disabled?: boolean;
}) {
  return (
    <View class="gap-1.5 px-4 py-3">
      {props.label ? <Text class="text-xs font-medium text-muted-foreground">{props.label}</Text> : null}
      <View class="flex-row items-center gap-2 rounded-xl border border-border bg-background px-3">
        <Input
          testID={props.testID}
          accessibilityLabel={props.accessibilityLabel ?? props.label}
          class={props.mono ? "min-h-[44px] flex-1 py-2.5 font-mono text-sm" : "min-h-[44px] flex-1 py-2.5 text-base"}
          value={props.value}
          placeholder={props.placeholder}
          masked={props.masked}
          multiline={props.multiline}
          keyboardType={props.keyboardType}
          autoFocus={props.autoFocus}
          disabled={props.disabled}
          autoCapitalize="none"
          autoCorrect={false}
          onChange={({ value }) => props.onChange(value)}
          onSubmit={() => (props.onSubmit ?? props.onCommit)?.()}
          onBlur={() => props.onCommit?.()}
        />
        {props.trailing ?? null}
      </View>
    </View>
  );
}

/** A page the new app has not built yet: it says so, so nothing goes missing silently. */
export function NotBuiltYet(props: { title: string }) {
  return (
    <Page title={props.title} testID="not-built-yet">
      <EmptyState title="Not built yet" detail={`${props.title} comes in a later build of this preview.`} />
    </Page>
  );
}

/**
 * What a long press on a value offers: a sheet of "Copy …" choices; the chosen one goes on
 * the clipboard. Stands in for selectable text, which the pages' texts are not.
 */
export async function copyMenu(title: string, choices: ReadonlyArray<{ label: string; value: string }>): Promise<void> {
  if (choices.length === 0) return;
  const answer = await AlertDialog.show({
    title,
    presentation: "actionSheet",
    actions: [...choices.map((choice, index) => ({ id: String(index), label: choice.label })), { id: "cancel", label: "Cancel", role: "cancel" as const }],
  });
  const chosen = answer.action && answer.action !== "cancel" ? choices[Number(answer.action)] : undefined;
  if (!chosen) return;
  try {
    await Clipboard.writeText(chosen.value);
    announce("Copied");
  } catch {
    // A refused copy is not worth a message.
  }
}

/** Asks before an irreversible step; resolves true when the person chose `action`. */
export async function confirmDestructive(title: string, message: string, action: string): Promise<boolean> {
  const answer = await AlertDialog.show({
    title,
    description: message,
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "go", label: action, role: "destructive" },
    ],
  });
  return answer.action === "go";
}
