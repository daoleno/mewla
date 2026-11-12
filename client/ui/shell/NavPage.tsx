import { colorScheme, context, platform, split, Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button, NavPage as BaseNavPage, type NavPageProps, type StatusBar } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { statusBarForeground } from "../../core/settings/statusBar";
import { pageColumnWidth, type PageColumn } from "../../core/web/places";
import { Icon } from "../kit/Icon";
import { wide } from "./layout";

/** The stack a page is on, for its header's Back (the shell provides it per page). */
export const PageStack = context<{ canGoBack: () => boolean; back: () => void }>({ canGoBack: () => false, back: () => {} });

export type AppNavPageProps = NavPageProps & {
  /** The page's column (client/DESIGN.md): its header row and, framed, its content share it. Default `"page"`. */
  column?: PageColumn;
  /** The content is held in the column (default); false for a page that centers its own inside a full-width scroll. */
  frame?: boolean;
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
  if (mobile) {
    // A phone never reaches 1024 pt: its pages stay the system's own, with nothing around their content.
    if (platform.idiom === "phone") {
      const [own, rest] = split(props, ["column", "frame", "class", "headerClass", "statusBar"]);
      return <BaseNavPage {...rest} class={`${INK} ${own.class ?? ""}`} headerClass={own.headerClass ?? HEADER} statusBar={pageStatusBar(own.statusBar)} />;
    }
    const [own, rest] = split(props, ["column", "frame", "class", "headerClass", "statusBar", "children"]);
    return (
      <BaseNavPage {...rest} class={`${INK} ${own.class ?? ""}`} headerClass={own.headerClass ?? HEADER} statusBar={pageStatusBar(own.statusBar)}>
        <Column width={width}>{own.children}</Column>
      </BaseNavPage>
    );
  }
  // Read once each, into the one header: forwarded too, they would be made twice.
  const [own, rest] = split(props, ["header", "headerLeft", "headerRight", "headerTitle", "column", "frame", "children", "class"]);
  const stack = PageStack();
  const headerWidth = () => pageColumnWidth(own.column ?? "page");
  return (
    <BaseNavPage {...rest} class={`${INK} ${own.class ?? ""}`}
      header={own.header ?? <PageHeader title={props.title} titleView={own.headerTitle} left={own.headerLeft} right={own.headerRight} width={headerWidth} canGoBack={stack.canGoBack} back={stack.back} />}>
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
}) {
  return (
    <View testID="page-header" class="h-[52px] justify-center border-b border-border bg-background px-4">
      <View class="w-full flex-row items-center gap-2 self-center" style={{ maxWidth: wide() ? props.width() ?? undefined : undefined }}>
        {props.canGoBack() ? (
          <Button testID="page-back" accessibilityLabel="Back" onPress={props.back} class="size-10 items-center justify-center rounded-full hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring">
            <Icon name="chevron-left" size={20} tone="ink" />
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
