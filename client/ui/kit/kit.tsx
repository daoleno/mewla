import type { AccessibilityRole, JSX, ScrollRef } from "nsh";
import { announce, Canvas, Clipboard, effect, platform, Scroll, signal, Text, untracked, View } from "nsh";
import { Button, Input, Switch } from "nsh/base";
import { PAGE_COLUMN_WIDTH } from "../../core/web/places";
import { wide } from "../shell/layout";
import { NavPage } from "../shell/NavPage";
import { color, type ColorName } from "./colors";
import { Icon, type IconName } from "./Icon";
import { spinnerRadius } from "../../core/more/spinner";
import { showAlert } from "../shell/alert";
import { haptic } from "../shell/haptics";

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
  /** The widest the content grows below 1024 pt, in points (720; a dashboard takes more). On wide every list page has the 680 column. */
  maxWidth?: number;
  /** The page's scroll view, for a page that brings something at its top into view. */
  scrollRef?: (ref: ScrollRef) => void;
  /** A panel beside the scrolling page (a wide screen's details), or nothing. */
  aside?: JSX.Element;
  /** Laid over the page, not scrolling with it (a toast). */
  overlay?: JSX.Element;
  children?: JSX.Element;
}) {
  // Its column sits in a full-width scroll, so the scrollbar stays at the window's edge; the header row shares it.
  const width = () => (wide() ? PAGE_COLUMN_WIDTH.page : (props.maxWidth ?? 720));
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
    <NavPage title={titleAway() ? props.title : ""} testID={props.testID} headerRight={props.headerRight} column="page" frame={false}>
      <View class="flex-1 bg-background">
        {withAside ? <View class="flex-1 flex-row">{scroll}{props.aside}</View> : scroll}
        {props.overlay ?? null}
      </View>
    </NavPage>
  );
}

/** How far the content scrolls before the display title counts as gone (its eyebrow and most of its line). */
export const DISPLAY_TITLE_AWAY = 56;

/**
 * A caption's line (SPEC §10.2 caption 13/18): a section label, an eyebrow, a day separator. One line; in a row it
 * takes the rest (`min-w-0 flex-1`), and a caption longer than its place ends in an ellipsis.
 */
export const CAPTION_LINE = "text-caption line-clamp-1";

/** A caption (`CAPTION_LINE`): soft, 600 unless `plain`; `inRow` in a row; `center` for a day separator. */
export function Caption(props: { text: string; plain?: boolean; center?: boolean; inRow?: boolean; class?: string; role?: AccessibilityRole; testID?: string }) {
  return (
    <Text testID={props.testID} role={props.role}
      class={`${CAPTION_LINE} ${props.inRow ? "min-w-0 flex-1" : props.center ? "self-stretch" : ""} text-muted-foreground ${props.plain ? "" : "font-semibold"} ${props.center ? "text-center" : ""} ${props.class ?? ""}`}>{props.text}</Text>
  );
}

/** A pattern page's top block (SPEC §15): an eyebrow (caption 13/600, soft) over the title at display 32/700. */
export function DisplayTitle(props: { title: string; eyebrow?: string; testID?: string; titleTestID?: string }) {
  return (
    <View testID={props.testID ?? "display-title"} class="gap-1 px-1 pb-2 pt-3">
      {props.eyebrow ? <Caption testID="display-eyebrow" text={props.eyebrow} /> : null}
      <Text testID={props.titleTestID} role="header" class="text-display font-bold tracking-display" style={{ numberOfLines: 2 }}>{props.title}</Text>
    </View>
  );
}

/**
 * A section label with what the section holds (SPEC §15 Grouped list): an icon, the name, the count at the right.
 * Caption 13/600, soft, inset 16, 24 above and 8 below; pressing it folds the section when it can.
 */
export function SectionLabel(props: { title: string; icon?: IconName; count?: number | string; testID?: string; onPress?: () => void; folded?: boolean; trailing?: JSX.Element; accessibilityLabel?: string }) {
  const body = (
    <View class="min-h-[44px] flex-row items-center gap-2 px-4 pb-2 pt-6">
      {props.icon ? <Icon name={props.icon} size={16} tone="soft" /> : null}
      <Caption role="header" inRow text={props.title} />
      {props.count !== undefined ? <Text class="text-caption font-semibold text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(props.count)}</Text> : null}
      {props.trailing ?? null}
      {props.onPress ? <Icon name={props.folded ? "chevron-right" : "chevron-down"} size={16} tone="faint" /> : null}
    </View>
  );
  if (!props.onPress) return <View testID={props.testID} accessibilityLabel={props.accessibilityLabel}>{body}</View>;
  // The fold is the label's own button; what sits at its end (a group's Stage all) stays its own.
  if (props.trailing) {
    return (
      <View testID={props.testID ? `${props.testID}-row` : undefined} class="min-h-[44px] flex-row items-center gap-2 pb-2 pl-4 pr-2 pt-6">
        <View testID={props.testID} role="button" accessible accessibilityLabel={props.accessibilityLabel} accessibilityState={{ expanded: !props.folded }} onPress={props.onPress} class="min-w-0 flex-1 flex-row items-center gap-2 active:opacity-70">
          {props.icon ? <Icon name={props.icon} size={16} tone="soft" /> : null}
          <Caption role="header" inRow text={props.title} />
          {props.count !== undefined ? <Text class="text-caption font-semibold text-muted-foreground" style={{ fontVariant: ["tabular-nums"] }}>{String(props.count)}</Text> : null}
          <Icon name={props.folded ? "chevron-right" : "chevron-down"} size={16} tone="faint" />
        </View>
        {props.trailing}
      </View>
    );
  }
  return (
    <View testID={props.testID} role="button" accessible accessibilityLabel={props.accessibilityLabel} accessibilityState={{ expanded: !props.folded }} onPress={props.onPress} class="active:opacity-70">{body}</View>
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
export function SearchField(props: { value: string; placeholder: string; onChange: (value: string) => void; accessibilityLabel?: string; testID?: string; autoFocus?: boolean }) {
  return (
    <View class="min-h-[44px] flex-row items-center gap-2 rounded-[14px] bg-card px-3">
      <Icon name="search" size={16} tone="soft" />
      <Input
        testID={props.testID}
        accessibilityLabel={props.accessibilityLabel ?? props.placeholder}
        class="min-h-[44px] flex-1 py-2 text-body"
        value={props.value}
        placeholder={props.placeholder}
        autoFocus={props.autoFocus}
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

/**
 * A section label (caption, soft), one surface group of rows with inset hair dividers, an optional footnote.
 * `label` puts a SectionLabel (icon, name, count) in place of the plain one; `sheet` draws the group on a sheet's surface.
 */
export function Section(props: { title?: string; detail?: JSX.Element; footer?: string; testID?: string; label?: JSX.Element; sheet?: boolean; children?: JSX.Element }) {
  return (
    <View class={props.label ? "" : "mt-6"} testID={props.testID}>
      {props.label ?? null}
      {!props.label && (props.title || props.detail) ? (
        <View class="mb-2 min-h-[18px] flex-row items-center justify-between px-4">
          <Caption role="header" inRow text={props.title ?? ""} />
          {props.detail ?? null}
        </View>
      ) : null}
      <View class={props.sheet ? "overflow-hidden rounded-(--radius-group) bg-sheet-surface" : "overflow-hidden rounded-(--radius-group) bg-card"}>
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
        class="hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-40"
      >
        {body}
      </View>
    );
  }
  return (
    <Button testID={props.testID} disabled={props.disabled} selected={props.selected} accessibilityLabel={props.accessibilityLabel} role={props.role} onPress={props.onPress} class="hover:bg-accent active:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:opacity-40">
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
/** A page's state (busy, empty, failed) and its one way on. `onBlack`: on the full-screen remote's black, in white. */
export function EmptyState(props: { title?: string; detail?: string; busy?: boolean; icon?: IconName; tone?: ColorName; action?: string; onAction?: () => void; testID?: string; onBlack?: boolean }) {
  return (
    <View testID={props.testID} class="items-center gap-2 px-6 py-16">
      {props.busy ? <Spinner size={24} tone={props.onBlack ? "onBlackSoft" : undefined} /> : props.icon ? <Icon name={props.icon} size={28} tone={props.onBlack ? "onBlack" : props.tone} /> : null}
      {props.title ? <Text class={`mt-1 text-center text-title font-semibold tracking-title ${props.onBlack ? "text-white" : ""}`}>{props.title}</Text> : null}
      {props.detail ? <Text class={props.onBlack ? "text-center text-sub text-white/60" : "text-center text-sub text-muted-foreground"}>{props.detail}</Text> : null}
      {props.action && props.onAction ? <PillButton testID={props.testID ? `${props.testID}-action` : undefined} title={props.action} onPress={props.onAction} kind={props.onBlack ? "white" : undefined} onCanvas /> : null}
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
  /** `white`: the one action on the full-screen remote's black. */
  kind?: "ink" | "ghost" | "seal" | "white";
  small?: boolean;
  onCanvas?: boolean;
  busy?: boolean;
  disabled?: boolean;
  destructive?: boolean;
  testID?: string;
}) {
  const kind = () => props.kind ?? (props.filled ? "ink" : "ghost");
  const fill = () => (kind() === "ink" ? "bg-primary" : kind() === "seal" ? "bg-seal" : kind() === "white" ? "bg-white" : props.onCanvas ? "bg-card" : "bg-muted");
  const size = () => (props.small ? "h-[48px] rounded-(--radius-control) px-4" : "h-[52px] rounded-2xl px-5");
  const ink = () => (kind() === "ink" ? "text-primary-foreground" : kind() === "seal" ? "text-seal-foreground" : kind() === "white" ? "text-black" : "text-foreground");
  return (
    <Button testID={props.testID} disabled={props.disabled || props.busy} onPress={props.onPress} class={`mt-2 flex-row items-center justify-center gap-2 active:opacity-70 disabled:opacity-40 ${size()} ${fill()}`}>
      {props.busy ? <Spinner size={16} tone={kind() === "ghost" ? "soft" : kind() === "white" ? "onBlackSoft" : "canvas"} /> : null}
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
      {props.label ? <Caption text={props.label} /> : null}
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
  const answer = await showAlert({
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
  haptic.notification("warning");
  const answer = await showAlert({
    title,
    description: message,
    actions: [
      { id: "cancel", label: "Cancel", role: "cancel" },
      { id: "go", label: action },
    ],
  });
  return answer.action === "go";
}
