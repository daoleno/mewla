import type { AccessibilityRole, JSX, ScrollRef } from "nsh";
import { announce, Canvas, Clipboard, effect, platform, Scroll, signal, Text, untracked, View } from "nsh";
import { AlertDialog, Button, Input, Switch } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import type { PageColumn } from "../../core/web/places";
import { NavPage } from "../shell/NavPage";
import { color, type ColorName } from "./colors";
import { Icon, type IconName } from "./Icon";
import { spinnerRadius } from "../../core/more/spinner";

/**
 * The building blocks of the pages: a page with the system header, groups of rows
 * under a section label, buttons, and the few states a page has (busy, empty,
 * failed). The Soft look of soft-final SPEC §10–§11: canvas, surface groups, ink.
 */

export function Page(props: {
  title: string;
  /**
   * The pattern pages' top block (SPEC §15): the title at display size in the content, with an optional
   * eyebrow over it (a range, a project, a path). The nav bar shows Back and the actions, and the title only
   * once the display title has scrolled away.
   */
  display?: boolean;
  eyebrow?: string;
  /** The display title when it is not the page's name (Stats' total, a folder's name). */
  displayTitle?: string;
  /** A detail page's own top block in place of the display title (a service's tile, name and state). */
  hero?: JSX.Element;
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
  /** Laid over the page, not scrolling with it (a toast). */
  overlay?: JSX.Element;
  children?: JSX.Element;
}) {
  // Its column sits in a full-width scroll, so the scrollbar stays at the window's edge; the header row shares it.
  const width = () => (mobile ? (props.maxWidth ?? 720) : props.column === "wide" ? 1200 : 760);
  // Whether the display title is on screen: the nav bar names the page once it has scrolled away.
  const display = untracked(() => Boolean(props.display));
  const titleAway = signal(!display);
  const scroll = (
    <Scroll keyboardShouldPersistTaps="handled" ref={props.scrollRef} class="flex-1 bg-background" contentClass="px-4 pb-10 pt-2" refreshing={props.refreshing} onRefresh={props.onRefresh}
      onScroll={display ? (e) => titleAway.set(e.offset.y > DISPLAY_TITLE_AWAY) : undefined} scrollEventThrottle={display ? 64 : undefined}>
      <View class="w-full self-center" style={{ maxWidth: width() }}>
        {display ? (props.hero ?? <DisplayTitle eyebrow={props.eyebrow} title={props.displayTitle ?? props.title} />) : null}
        {props.children}
      </View>
    </Scroll>
  );
  // Whether the page has a side panel is fixed when it is made; what the panel shows is not.
  const withAside = untracked(() => props.aside !== undefined);
  return (
    <NavPage title={titleAway() ? props.title : ""} testID={props.testID} headerRight={props.headerRight} column={props.column ?? "page"} frame={false}>
      <View class="flex-1 bg-background">
        {withAside ? <View class="flex-1 flex-row">{scroll}{props.aside}</View> : scroll}
        {props.overlay ?? null}
      </View>
    </NavPage>
  );
}

/** How far the content scrolls before the display title counts as gone (its eyebrow and most of its line). */
const DISPLAY_TITLE_AWAY = 56;

/** A pattern page's top block (SPEC §15): an eyebrow (caption 13/600, soft) over the title at display 32/700. */
export function DisplayTitle(props: { title: string; eyebrow?: string; testID?: string }) {
  return (
    <View testID={props.testID ?? "display-title"} class="gap-1 px-1 pb-2 pt-3">
      {props.eyebrow ? <Text class="text-caption font-semibold text-muted-foreground line-clamp-1">{props.eyebrow}</Text> : null}
      <Text role="header" class="text-display font-bold tracking-display" style={{ numberOfLines: 2 }}>{props.title}</Text>
    </View>
  );
}

/**
 * A section label with what the section holds (SPEC §15 Grouped list): an icon, the name, the count at the right.
 * Caption 13/600, soft, inset 16, 24 above and 8 below; pressing it folds the section when it can.
 */
export function SectionLabel(props: { title: string; icon?: IconName; count?: number | string; testID?: string; onPress?: () => void; folded?: boolean; trailing?: JSX.Element }) {
  const body = (
    <View class="min-h-[44px] flex-row items-center gap-2 px-4 pb-2 pt-6">
      {props.icon ? <Icon name={props.icon} size={16} tone="soft" /> : null}
      <Text class="min-w-0 flex-1 text-caption font-semibold text-muted-foreground line-clamp-1" role="header">{props.title}</Text>
      {props.count !== undefined ? <Text class="text-caption font-semibold text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(props.count)}</Text> : null}
      {props.trailing ?? null}
      {props.onPress ? <Icon name={props.folded ? "chevron-right" : "chevron-down"} size={16} tone="faint" /> : null}
    </View>
  );
  if (!props.onPress) return <View testID={props.testID}>{body}</View>;
  return (
    <View testID={props.testID} role="button" accessible accessibilityState={{ expanded: !props.folded }} onPress={props.onPress} class="active:opacity-70">{body}</View>
  );
}

/** A square tile (SPEC §15): a glyph or a short mark on the canvas, radius 11 (36 pt) or 8 (28 pt). */
export function Tile(props: { size?: 28 | 36 | 64; icon?: IconName; mark?: string; children?: JSX.Element; testID?: string }) {
  const size = () => props.size ?? 36;
  const radius = () => (size() === 64 ? 18 : size() === 36 ? 11 : 8);
  return (
    <View testID={props.testID} class="items-center justify-center bg-background" style={{ width: size(), height: size(), borderRadius: radius() }}>
      {props.children ?? (props.icon ? <Icon name={props.icon} size={size() === 64 ? 32 : size() === 36 ? 20 : 16} tone="ink" /> : props.mark ? <Text class="font-mono text-caption font-semibold">{props.mark}</Text> : null)}
    </View>
  );
}

/** The search field under a collection's title (SPEC §15): 44 pt on the surface, radius 14, a clear button once it holds text. */
export function SearchField(props: { value: string; placeholder: string; onChange: (value: string) => void; accessibilityLabel?: string; testID?: string }) {
  return (
    <View class="min-h-[44px] flex-row items-center gap-2 rounded-[14px] bg-card px-3">
      <Icon name="search" size={16} tone="soft" />
      <Input
        testID={props.testID}
        accessibilityLabel={props.accessibilityLabel ?? props.placeholder}
        class="min-h-[44px] flex-1 py-2 text-body"
        value={props.value}
        placeholder={props.placeholder}
        autoCapitalize="none"
        autoCorrect={false}
        onChange={({ value }) => props.onChange(value)}
      />
      {props.value ? (
        <Button accessibilityLabel="Clear search" onPress={() => props.onChange("")} class="size-8 items-center justify-center active:opacity-60">
          <Icon name="close-circle" size={16} tone="faint" />
        </Button>
      ) : null}
    </View>
  );
}

/** A small ghost button on a row (Connect, Reconnect, Allow): 32 pt, canvas pill, 15/600. */
export function RowButton(props: { label: string; onPress: () => void; disabled?: boolean; accessibilityLabel?: string; testID?: string }) {
  return (
    <Button testID={props.testID} accessibilityLabel={props.accessibilityLabel ?? props.label} disabled={props.disabled} onPress={props.onPress} hitSlop={6} class="h-8 items-center justify-center rounded-full bg-background px-3.5 active:opacity-70 disabled:opacity-40">
      <Text class="text-sub font-semibold">{props.label}</Text>
    </Button>
  );
}

/**
 * A stat tile (SPEC §15 Metrics): label 13/600 soft, value 28/700 (soft while it is not known), a 13 soft line,
 * an optional 4 pt bar, and whatever the tile shows under them (a small history).
 */
export function StatTile(props: { label: string; value: string; detail?: string; share?: number; muted?: boolean; testID?: string; valueTestID?: string; children?: JSX.Element }) {
  return (
    <View testID={props.testID} class="min-w-[140px] flex-1 gap-0.5 rounded-(--radius-group) bg-card p-4">
      <Text class="text-caption font-semibold text-muted-foreground line-clamp-1">{props.label}</Text>
      <Text testID={props.valueTestID} class={props.muted ? "text-[28px] font-bold leading-[34px] text-muted-foreground line-clamp-1" : "text-[28px] font-bold leading-[34px] line-clamp-1"} style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
      {props.detail ? <Text class="text-caption text-muted-foreground line-clamp-2">{props.detail}</Text> : null}
      {props.share !== undefined ? <View class="mt-2"><Meter share={props.share} /></View> : null}
      {props.children ? <View class="mt-2 gap-2">{props.children}</View> : null}
    </View>
  );
}

/** Stat tiles two across on a phone, four on wide (SPEC §15 Metrics); 12 between them. */
export function StatGrid(props: { testID?: string; children?: JSX.Element }) {
  return <View testID={props.testID} class="mt-3 flex-row flex-wrap gap-3">{props.children}</View>;
}

/** A ranked row (SPEC §15 Metrics): a glyph, the name over its second line, a 72 pt bar of its share, the value. */
export function RankedRow(props: { title: string; subtitle?: string; share: number; value: string; icon?: IconName; leading?: JSX.Element; testID?: string; accessibilityLabel?: string; accessibilityHint?: string; onPress?: () => void }) {
  return (
    <Row
      testID={props.testID}
      icon={props.icon}
      leading={props.leading}
      title={props.title}
      subtitle={props.subtitle}
      accessibilityLabel={props.accessibilityLabel}
      accessibilityHint={props.accessibilityHint}
      onPress={props.onPress}
      chevron={Boolean(props.onPress)}
      trailing={
        <View class="flex-row items-center gap-3">
          <View class="w-[72px]"><Meter share={props.share} /></View>
          <Text class="min-w-[56px] text-right text-sub text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text>
        </View>
      }
    />
  );
}

/** A section label (caption, soft), one surface group of rows with inset hair dividers, an optional footnote. */
export function Section(props: { title?: string; detail?: JSX.Element; footer?: string; testID?: string; children?: JSX.Element }) {
  return (
    <View class="mt-6" testID={props.testID}>
      {props.title || props.detail ? (
        <View class="mb-2 min-h-[18px] flex-row items-center justify-between px-4">
          <Text class="text-caption font-semibold text-muted-foreground" role="header">{props.title ?? ""}</Text>
          {props.detail ?? null}
        </View>
      ) : null}
      <View class="overflow-hidden rounded-(--radius-group) bg-card">
        <View class="-mb-px">{props.children}</View>
      </View>
      {props.footer ? <Text class="mt-2 px-4 text-caption text-muted-foreground">{props.footer}</Text> : null}
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
    <View class="min-h-[56px] flex-row items-center gap-3 px-4 py-3">
      {props.leading ?? (props.icon ? <Icon name={props.icon} size={24} tone={props.destructive ? "ink" : "soft"} /> : null)}
      <View class="min-w-0 flex-1">
        <Text class={props.destructive ? "text-body text-destructive" : props.mono ? "font-mono text-code" : "text-body"} style={{ numberOfLines: 2 }}>{props.title}</Text>
        {props.subtitle ? <Text class="mt-0.5 text-sub text-muted-foreground line-clamp-3">{props.subtitle}</Text> : null}
      </View>
      {props.value ? <Text class="max-w-[50%] text-right text-body text-muted-foreground line-clamp-1" style={{ fontVariant: ["tabular-nums"] }}>{props.value}</Text> : null}
      {props.trailing ?? null}
      {props.check ? <Icon name="check" tone="ink" size={20} /> : null}
      {props.chevron ? <Icon name="chevron-right" size={18} tone="faint" /> : null}
      <Hairline inset={props.icon || props.leading ? 52 : 16} />
    </View>
  );
  if (!props.onPress && !props.onLongPress) {
    return (
      <View testID={props.testID} class={props.disabled ? "opacity-40" : ""} accessible={props.accessibilityLabel ? true : undefined} accessibilityLabel={props.accessibilityLabel} role={props.role}>
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
        class="active:bg-accent disabled:opacity-40"
      >
        {body}
      </View>
    );
  }
  return (
    <Button testID={props.testID} disabled={props.disabled} selected={props.selected} accessibilityLabel={props.accessibilityLabel} role={props.role} onPress={props.onPress} class="active:bg-accent disabled:opacity-40">
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
            class="disabled:opacity-40"
            trackClass="h-[30px] w-[50px] rounded-full bg-track checked:bg-primary"
            thumbClass="m-[2px] size-[26px] rounded-full bg-card checked:translate-x-[20px]"
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
        const r = spinnerRadius(s.width, s.height);
        if (r === null) return;
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
      {props.title ? <Text class="mt-1 text-center text-title font-semibold tracking-title">{props.title}</Text> : null}
      {props.detail ? <Text class="text-center text-sub text-muted-foreground">{props.detail}</Text> : null}
      {props.action && props.onAction ? <PillButton testID={props.testID ? `${props.testID}-action` : undefined} title={props.action} onPress={props.onAction} onCanvas /> : null}
    </View>
  );
}

/**
 * A button (SPEC §11): `ink` when it is the one action, `ghost` otherwise, `seal` for Approve only.
 * 52 high (48 `small`), radius 16 (14 small), 17/600, never wrapping; pressed .7, disabled 40 %.
 * A ghost button is canvas inside a surface group, surface on the canvas (`onCanvas`).
 */
export function PillButton(props: {
  title: string;
  onPress: () => void;
  /** The ink button: the page's or the group's one action. */
  filled?: boolean;
  kind?: "ink" | "ghost" | "seal";
  small?: boolean;
  onCanvas?: boolean;
  busy?: boolean;
  disabled?: boolean;
  destructive?: boolean;
  testID?: string;
}) {
  const kind = () => props.kind ?? (props.filled ? "ink" : "ghost");
  const fill = () => (kind() === "ink" ? "bg-primary" : kind() === "seal" ? "bg-seal" : props.onCanvas ? "bg-card" : "bg-muted");
  const size = () => (props.small ? "h-[48px] rounded-(--radius-control) px-4" : "h-[52px] rounded-2xl px-5");
  const ink = () => (kind() === "ink" ? "text-primary-foreground" : kind() === "seal" ? "text-seal-foreground" : "text-foreground");
  return (
    <Button testID={props.testID} disabled={props.disabled || props.busy} onPress={props.onPress} class={`mt-2 flex-row items-center justify-center gap-2 active:opacity-70 disabled:opacity-40 ${size()} ${fill()}`}>
      {props.busy ? <Spinner size={16} tone={kind() === "ghost" ? "soft" : "canvas"} /> : null}
      <Text class={`text-body font-semibold line-clamp-1 ${ink()}`}>{props.title}</Text>
    </Button>
  );
}

const NOTICE_GLYPH: Record<"warning" | "failed" | "success" | "info", { icon: IconName; tone: ColorName }> = {
  warning: { icon: "alert", tone: "soft" },
  failed: { icon: "alert", tone: "ink" },
  success: { icon: "check-circle", tone: "soft" },
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
    <View testID={props.testID} android={{ accessibilityLiveRegion: props.live ? "polite" : "none" }} class="mt-4 min-h-[68px] gap-3 rounded-(--radius-group) bg-card px-4 py-3.5">
      <View class="flex-row items-start gap-3.5">
        {props.busy ? <Spinner size={24} /> : <Icon name={props.icon ?? glyph().icon} size={24} tone={glyph().tone} />}
        <View class="flex-1">
          <Text class="text-body font-semibold">{props.title}</Text>
          {props.detail ? <Text class="mt-0.5 text-sub text-muted-foreground">{props.detail}</Text> : null}
        </View>
      </View>
      {props.action && props.onAction ? (
        <View class="flex-row flex-wrap gap-2 pl-[38px]">
          <PillButton testID={props.testID ? `${props.testID}-action` : undefined} title={props.action} kind="ink" small disabled={props.actionDisabled || props.busy} onPress={props.onAction} />
        </View>
      ) : null}
    </View>
  );
}

export type Status = "ready" | "running" | "warning" | "failed" | "blocked" | "neutral" | "needs";

/** A state's glyph and word: the shape tells the state, so it reads in ink and soft alone (red only for needs you). */
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
            cx.strokeStyle = color("surface");
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
      {props.label ? <Text class="text-sub text-muted-foreground">{props.label}</Text> : null}
    </View>
  );
}

/** A quiet label on a row (not the red request tag): caption 600, soft, on a canvas pill. */
export function Tag(props: { label: string }) {
  return (
    <View class="rounded-full bg-muted px-2.5 py-1">
      <Text class="text-caption font-semibold text-muted-foreground">{props.label}</Text>
    </View>
  );
}

/** "Show N more items" / "Show fewer items" under a long card. */
export function ShowMore(props: { hidden: number; expanded: boolean; onToggle: () => void; testID?: string }) {
  return (
    <Button testID={props.testID} onPress={props.onToggle} class="min-h-[52px] justify-center px-4 active:bg-accent">
      <Text class="text-sub text-muted-foreground">{props.expanded ? "Show fewer items" : `Show ${props.hidden} more ${props.hidden === 1 ? "item" : "items"}`}</Text>
    </Button>
  );
}

/** A thin bar: `share` of the width filled in ink, on the track. */
export function Meter(props: { share: number; tone?: ColorName; height?: number }) {
  return (
    <View class="overflow-hidden rounded-full bg-track" style={{ height: props.height ?? 4 }}>
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
      {props.label ? <Text class="text-caption font-semibold text-muted-foreground">{props.label}</Text> : null}
      <View class="flex-row items-center gap-2 rounded-(--radius-control) bg-background px-3">
        <Input
          testID={props.testID}
          accessibilityLabel={props.accessibilityLabel ?? props.label}
          class={props.mono ? "min-h-[48px] flex-1 py-3 font-mono text-code" : "min-h-[48px] flex-1 py-3 text-body"}
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

/**
 * Asks before an irreversible step (the system alert); resolves true when the person chose `action`.
 * The action keeps the normal role: red is for what waits for you, so iOS must not paint it.
 */
export async function confirmDestructive(title: string, message: string, action: string): Promise<boolean> {
  const answer = await AlertDialog.show({
    title,
    description: message,
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "go", label: action },
    ],
  });
  return answer.action === "go";
}
