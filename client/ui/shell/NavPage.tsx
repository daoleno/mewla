import { context, split, Text, View } from "nsh";
import type { JSX } from "nsh";
import { Button, NavPage as BaseNavPage, type NavPageProps } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { pageColumnWidth, type PageColumn } from "../../core/web/places";
import { Icon } from "../more/kit/Icon";

/** The stack a page is on, for its header's Back (the shell provides it per page). */
export const PageStack = context<{ canGoBack: () => boolean; back: () => void }>({ canGoBack: () => false, back: () => {} });

export type AppNavPageProps = NavPageProps & {
  /** The page's column (app/DESIGN.md): its header row and, framed, its content share it. Default `"page"`. */
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

/**
 * The app's page. iOS and Android draw their system header from `title`,
 * `headerLeft`, `headerTitle` and `headerRight`; the desktops and the web draw
 * none, so there these become the page's own 52 pt header: Back when the page
 * is not its stack's first, the title, the actions on the right, on the page's
 * centered column so the title lines up with the content below it.
 */
export function NavPage(props: AppNavPageProps) {
  if (mobile) {
    const [own, rest] = split(props, ["column", "frame", "class", "headerClass"]);
    return <BaseNavPage {...rest} class={`${INK} ${own.class ?? ""}`} headerClass={own.headerClass ?? HEADER} />;
  }
  // Read once each, into the one header: forwarded too, they would be made twice.
  const [own, rest] = split(props, ["header", "headerLeft", "headerRight", "headerTitle", "column", "frame", "children", "class"]);
  const stack = PageStack();
  const width = () => pageColumnWidth(own.column ?? "page");
  return (
    <BaseNavPage {...rest} class={`${INK} ${own.class ?? ""}`}
      header={own.header ?? <PageHeader title={props.title} titleView={own.headerTitle} left={own.headerLeft} right={own.headerRight} width={width()} canGoBack={stack.canGoBack} back={stack.back} />}>
      {own.frame === false || width() === null
        ? own.children
        : <View class="w-full flex-1 self-center" style={{ maxWidth: width()! }}>{own.children}</View>}
    </BaseNavPage>
  );
}

function PageHeader(props: {
  title?: string;
  titleView?: JSX.Element;
  left?: JSX.Element;
  right?: JSX.Element;
  width: number | null;
  canGoBack: () => boolean;
  back: () => void;
}) {
  return (
    <View testID="page-header" class="h-[52px] justify-center border-b border-border bg-background px-4">
      <View class="w-full flex-row items-center gap-2 self-center" style={props.width ? { maxWidth: props.width } : undefined}>
        {props.canGoBack() ? (
          <Button testID="page-back" accessibilityLabel="Back" onPress={props.back} class="size-10 items-center justify-center rounded-full hover:bg-accent">
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
