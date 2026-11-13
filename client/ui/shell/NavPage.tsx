import { colorScheme, context, effect, platform, safeArea, split, Text, untracked, View } from "nsh";
import type { JSX } from "nsh";
import { Button, NavPage as BaseNavPage, type NavPageProps, type StatusBar } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { CAPSULE, capsulePageInset } from "../../core/shell/capsule";
import { statusBarForeground } from "../../core/settings/statusBar";
import { backMark } from "../../core/tray/model";
import { nativeHeaderTitleWidth, pageColumnWidth, type PageColumn } from "../../core/web/places";
import { tray } from "../tray/state";
import { Icon } from "../kit/Icon";
import { Capsule, createCapsule } from "./Capsule";
import { stackWidth, wide } from "./layout";

/** The stack a page is on, for its header's Back (the shell provides it per page). */
export const PageStack = context<{ canGoBack: () => boolean; back: () => void }>({ canGoBack: () => false, back: () => {} });

export type AppNavPageProps = NavPageProps & {
  /** The page's column (client/DESIGN.md): its header row and, framed, its content share it. Default `"page"`. */
  column?: PageColumn;
  /** The content is held in the column (default); false for a page that centers its own inside a full-width scroll. */
  frame?: boolean;
  /**
   * On a phone, the page's capsule header (`shell/Capsule.tsx`) instead of the system header: the system
   * header is hidden, the content starts at the screen's top and the capsule floats over it. The desktops
   * and the web keep the page's header from `headerTitle`.
   */
  capsule?: JSX.Element;
  /**
   * A pushed page on a phone pads its own scroll for the capsule (`capsulePageInset`) and its content scrolls
   * under it; without, the page's content starts below the capsule.
   */
  capsuleUnder?: boolean;
  /** Whether the page has controls of its own for the capsule (default: whether it passes `headerRight`). */
  capsuleControls?: boolean;
};

/**
 * NSH draws a Text with no color of its own black, and the system header in the platform's light
 * colors: in Dark both were unreadable on the dark page (FINAL-VERIFY 6387f88f settings [94]). A
 * page gives its texts the theme's ink (a text's or a button's own color still wins), and its
 * system header the page's paper and ink in both appearances.
 */
const INK = "text-foreground";
const HEADER = "bg-background text-foreground tint-foreground";

/** A page's status bar: its content in the app's appearance (`core/settings/statusBar.ts`), then the page's own fields. Read in the JSX, so it follows a change of appearance. */
export function pageStatusBar(own?: StatusBar): StatusBar {
  return { foreground: statusBarForeground(colorScheme() === "dark" ? "dark" : "light"), ...own };
}

/**
 * The page's content in its column (client/DESIGN.md): always the same view, so a page that changes
 * column (a Session between chat and terminal) or a window crossing 1024 never makes its content
 * again; only the width changes. On a phone and a narrow window the whole width.
 */
function Column(props: { width: () => number | null; children: JSX.Element }) {
  return <View class="w-full flex-1 self-center" style={{ maxWidth: wide() ? props.width() ?? undefined : undefined }}>{props.children}</View>;
}

/**
 * The app's page. iOS and Android draw their system header from `title`,
 * `headerLeft`, `headerTitle` and `headerRight`; the desktops and the web draw
 * none, so there these become the page's own 52 pt header: Back when the page
 * is not its stack's first, the title, the actions on the right, on the page's
 * centered column so the title lines up with the content below it. On wide
 * (any device) the content sits in its column: 680 for a list page, 760 for a
 * thread, the whole pane for the terminal, a diff and Screen (SPEC §14.1).
 */
export function NavPage(props: AppNavPageProps) {
  const width = () => (props.frame === false ? null : pageColumnWidth(props.column ?? "page"));
  const stack = PageStack();
  /** Back's connection mark on a pushed page (SPEC §1.3); null when connected or on the stack's first page. */
  const mark = () => (stack.canGoBack() ? backMark(tray().state.kind) : null);
  const phone = mobile && platform.idiom === "phone";
  if (phone && "capsule" in props) {
    const [own, rest] = split(props, ["capsule", "capsuleUnder", "capsuleControls", "headerLeft", "headerTitle", "headerRight", "column", "frame", "class", "headerClass", "statusBar", "children"]);
    return (
      <BaseNavPage {...rest} headerVisible={false} class={`${INK} ${own.class ?? ""}`} statusBar={pageStatusBar(own.statusBar)}>
        <View class="flex-1">
          {own.children}
          {own.capsule}
        </View>
      </BaseNavPage>
    );
  }
  // Every other pushed page on a phone (SPEC §11: Settings, the computer page, every pushed page): the capsule
  // holds Back, and the title and the page's controls whenever the page's own title is not on screen. A page that
  // shows and hides its header itself (Screen's full bleed, black in both themes) keeps the system's.
  if (phone && !("headerVisible" in props) && untracked(stack.canGoBack)) {
    const [own, rest] = split(props, ["capsuleUnder", "capsuleControls", "headerLeft", "headerTitle", "headerRight", "column", "frame", "class", "headerClass", "statusBar", "children"]);
    const under = untracked(() => Boolean(own.capsuleUnder));
    const controls = "capsuleControls" in props ? untracked(() => Boolean(own.capsuleControls)) : "headerRight" in props;
    return (
      <BaseNavPage {...rest} headerVisible={false} class={`${INK} ${own.class ?? ""}`} statusBar={pageStatusBar(own.statusBar)}>
        <View class="flex-1">
          {under ? own.children : <View class="flex-1" style={{ paddingTop: capsulePageInset(safeArea().top) }}>{own.children}</View>}
          <PageCapsule title={() => props.title ?? ""} controls={controls} right={() => own.headerRight} />
        </View>
      </BaseNavPage>
    );
  }
  if (mobile) {
    // The system's Back can't carry a dot: while the mark shows, the page's own Back stands in for it.
    const [slots, page] = split(props, ["headerLeft", "headerTitle"]);
    const markedBack = () => (mark() ? <MarkedBack mark={mark()!} back={stack.back} /> : null);
    // Android draws the header's leading item after the title: there Back and the leading item go into
    // the title's place, ahead of the title, so they lead as on iOS. Read by key, the slots are made once.
    const android = platform.os === "android";
    const hasLeft = "headerLeft" in props;
    // Android insets a custom title 72 dp after its own Back, a wide gap before a Session's logo: a page
    // with its own title view draws Back itself, in the title's place, right before it.
    const ownTitle = "headerTitle" in props;
    const ownBack = () => android && Boolean(mark() || (ownTitle && stack.canGoBack()));
    const back = () => (mark() ? markedBack() : ownBack() ? <PlainBack back={stack.back} /> : null);
    const left = () => (android ? undefined : mark() ? <View class="flex-row items-center gap-2">{markedBack()}{slots.headerLeft ?? null}</View> : slots.headerLeft);
    // A page's own title view is as wide as the bar, not as its content (`nativeHeaderTitleWidth`): its
    // controls stay on screen and its text ends in "…" before them.
    const titleWidth = () => (ownTitle ? nativeHeaderTitleWidth({ stackWidth: stackWidth(), os: platform.os, systemBack: stack.canGoBack() && !ownBack() }) : undefined);
    const title = () =>
      android && (ownBack() || hasLeft) ? (
        <View class={`min-w-0 flex-1 flex-row items-center ${ownTitle ? "gap-1" : "gap-4"}`} style={{ width: titleWidth() }}>
          {back()}
          {slots.headerLeft ?? null}
          {slots.headerTitle ?? <Text role="header" class="shrink text-[22px] font-medium text-foreground line-clamp-1">{page.title ?? ""}</Text>}
        </View>
      ) : ownTitle ? <View class="flex-row items-center" style={{ width: titleWidth() }}>{slots.headerTitle}</View> : slots.headerTitle;
    // A phone never reaches 1024 pt: its pages stay the system's own, with nothing around their content.
    if (platform.idiom === "phone") {
      const [own, rest] = split(page, ["capsuleUnder", "capsuleControls", "column", "frame", "class", "headerClass", "statusBar"]);
      return <BaseNavPage {...rest} headerBackVisible={!ownBack() && !mark()} headerLeft={left()} headerTitle={title()} class={`${INK} ${own.class ?? ""}`} headerClass={own.headerClass ?? HEADER} statusBar={pageStatusBar(own.statusBar)} />;
    }
    const [own, rest] = split(page, ["capsuleUnder", "capsuleControls", "column", "frame", "class", "headerClass", "statusBar", "children"]);
    return (
      <BaseNavPage {...rest} headerBackVisible={!ownBack() && !mark()} headerLeft={left()} headerTitle={title()} class={`${INK} ${own.class ?? ""}`} headerClass={own.headerClass ?? HEADER} statusBar={pageStatusBar(own.statusBar)}>
        <Column width={width}>{own.children}</Column>
      </BaseNavPage>
    );
  }
  // Read once each, into the one header: forwarded too, they would be made twice.
  const [own, rest] = split(props, ["header", "capsuleUnder", "capsuleControls", "headerLeft", "headerRight", "headerTitle", "column", "frame", "children", "class"]);
  const headerWidth = () => pageColumnWidth(own.column ?? "page");
  return (
    <BaseNavPage {...rest} class={`${INK} ${own.class ?? ""}`}
      header={own.header ?? <PageHeader title={props.title} titleView={own.headerTitle} left={own.headerLeft} right={own.headerRight} width={headerWidth} canGoBack={stack.canGoBack} back={stack.back} mark={mark} />}>
      <Column width={width}>{own.children}</Column>
    </BaseNavPage>
  );
}

function PageHeader(props: {
  title?: string;
  titleView?: JSX.Element;
  left?: JSX.Element;
  right?: JSX.Element;
  width: () => number | null;
  canGoBack: () => boolean;
  back: () => void;
  mark: () => ReturnType<typeof backMark>;
}) {
  return (
    <View testID="page-header" class="h-[52px] justify-center border-b border-border bg-background px-4">
      <View class="w-full flex-row items-center gap-2 self-center" style={{ maxWidth: wide() ? props.width() ?? undefined : undefined }}>
        {props.canGoBack() ? (
          <Button testID="page-back" accessibilityLabel={props.mark()?.label ?? "Back"} onPress={props.back} class="size-10 items-center justify-center rounded-full hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2">
            <Icon name="chevron-left" size={20} tone="ink" />
            {props.mark() ? <MarkDot tone={props.mark()!.tone} /> : null}
          </Button>
        ) : null}
        {props.left ?? null}
        <View class="min-w-0 flex-1">
          {props.titleView ?? <Text role="header" class="text-xl font-semibold line-clamp-1">{props.title ?? ""}</Text>}
        </View>
        {props.right ? <View class="flex-row items-center gap-3">{props.right}</View> : null}
      </View>
    </View>
  );
}

/**
 * A pushed page's capsule (SPEC §11): Back alone, a 44 circle, while the page's own title is on screen (its
 * `title` is empty then); with the title, Back, the title 17/600 and the page's controls. A page with
 * controls of its own keeps them in reach: its capsule holds them with or without the title. It never
 * collapses while the person reads.
 */
function PageCapsule(props: { title: () => string; controls: boolean; right: () => JSX.Element | undefined }) {
  const alone = () => !props.title() && !props.controls;
  const capsule = createCapsule({ collapsed: untracked(alone) });
  effect(() => {
    const backOnly = alone();
    untracked(() => (backOnly ? capsule.collapse() : capsule.expand()));
  });
  return (
    <Capsule collapsed={capsule.collapsed} alone={() => true}>
      <CapsuleBack />
      {capsule.collapsed() ? null : (
        <>
          <Text testID="page-title" role="header" class="min-w-0 flex-1 font-semibold text-foreground line-clamp-1"
            style={{ fontSize: CAPSULE.pageTitle.size, lineHeight: CAPSULE.pageTitle.line, marginLeft: 4 }}>{props.title()}</Text>
          {props.controls ? <View class="flex-row items-center pr-1">{props.right() ?? null}</View> : null}
        </>
      )}
    </Capsule>
  );
}

/** A capsule's Back (`shell/Capsule.tsx`), 44 pt, with the connection mark while it shows (SPEC §1.3). */
export function CapsuleBack(props: { testID?: string }) {
  const stack = PageStack();
  const mark = () => (stack.canGoBack() ? backMark(tray().state.kind) : null);
  return (
    <Button testID={props.testID ?? "page-back"} accessibilityLabel={mark()?.label ?? "Back"} onPress={stack.back} class="size-11 items-center justify-center rounded-full active:bg-accent">
      {platform.os === "ios" ? <Icon name="chevron-left" size={22} tone="ink" /> : <View style={{ transform: [{ scaleX: -1 }] }}><Icon name="arrow-forward" size={22} tone="ink" /></View>}
      {mark() ? <MarkDot tone={mark()!.tone} /> : null}
    </Button>
  );
}

/** The 8 pt connection dot at Back's top-right corner (SPEC §1.3). */
function MarkDot(props: { tone: "grey" | "red" }) {
  return <View testID="back-mark" class={`absolute right-1.5 top-1.5 size-2 rounded-full ${props.tone === "red" ? "bg-seal" : "bg-muted-foreground"}`} />;
}

/** Android: the page's own Back, where a custom title view needs it close (the system's has the mark's place otherwise). */
function PlainBack(props: { back: () => void }) {
  return (
    <Button testID="page-back" accessibilityLabel="Back" onPress={props.back} class="size-10 items-center justify-center rounded-full active:bg-accent">
      <View style={{ transform: [{ scaleX: -1 }] }}><Icon name="arrow-forward" size={22} tone="ink" /></View>
    </Button>
  );
}

/** iOS and Android: Back with the connection mark, leading the system header while the mark shows (on Android from the title's place). */
function MarkedBack(props: { mark: { tone: "grey" | "red"; label: string }; back: () => void }) {
  return (
    <Button testID="page-back" accessibilityLabel={props.mark.label} onPress={props.back} class="size-10 items-center justify-center rounded-full active:bg-accent">
      {platform.os === "ios" ? <Icon name="chevron-left" size={22} tone="ink" /> : <View style={{ transform: [{ scaleX: -1 }] }}><Icon name="arrow-forward" size={22} tone="ink" /></View>}
      <MarkDot tone={props.mark.tone} />
    </Button>
  );
}
