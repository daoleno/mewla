import { safeArea, Text, View } from "nsh";
import type { JSX } from "nsh";
import { AppSheet } from "../shell/AppSheet";
import { wide } from "../shell/layout";

/**
 * The Soft sheet (soft-final/SPEC.md §11 Sheet): rises from the bottom on the sheet
 * canvas with 28 pt top corners, at most 88 % of the height, over the scrim; a drawn
 * 36 × 5 grabber 8 pt from the top; an optional title row (20/700, padding 11/16/19).
 * It closes on the scrim, Back and a swipe. Groups inside use `bg-sheet-surface`.
 *
 * One sheet can show several pages (Brain's menu, then "Brain host" or "Model"): the
 * owner swaps `children` and `title` while it stays open. Built on AppSheet, so a
 * switch of computer closes it and on wide it is a centred dialog without the grabber.
 */
export function Sheet(props: {
  open: () => boolean;
  onClose: () => void;
  testID?: string;
  title?: string | (() => string);
  /** Left of the title (a page's Back). */
  leading?: JSX.Element;
  /** Right of the title (a Close). */
  trailing?: JSX.Element;
  children?: JSX.Element;
}) {
  const title = () => (typeof props.title === "function" ? props.title() : props.title ?? "");
  return (
    <AppSheet open={props.open()} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe
      popupClass="max-h-[88%] rounded-t-(--radius-sheet) bg-sheet shadow-float" backdropClass="bg-scrim">
      {/* The sheet spans the screen: its content keeps clear of the side safe areas (an island, a status rail). */}
      <View testID={props.testID} class={wide() ? "pb-6 pt-3" : "pb-8"} style={wide() ? undefined : { paddingLeft: safeArea().left, paddingRight: safeArea().right }}>
        {/* A dialog on wide has no grabber (SPEC §14.2). */}
        <If when={!wide()}><View class="items-center pt-2">
            <View class="h-[5px] w-[36px] rounded-full bg-track" />
          </View></If>
        <If when={title() || props.leading || props.trailing} else={<View class="h-4" />}><View class="min-h-[44px] flex-row items-center gap-2 px-4 pb-[19px] pt-[11px]">
            {props.leading ?? null}
            <Text class="flex-1 text-[20px] font-bold text-foreground line-clamp-1">{title()}</Text>
            {props.trailing ?? null}
          </View></If>
        {props.children ?? null}
      </View>
    </AppSheet>
  );
}
