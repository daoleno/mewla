import { context, keyboardFrame, signal, Text, View, windowSize } from "nsh";
import type { JSX } from "nsh";
import { DIALOG_FOOT, dialogBodyRoom, sheetBodyRoom, sheetFoot, sheetTitleGivesWay } from "../../core/shell/sheet";
import { AppSheet, dialogInsets } from "../shell/AppSheet";
import { wide } from "../shell/layout";

/**
 * The height the open sheet's body may take (core/shell/sheet.ts): its cap less the keyboard, its header and its
 * foot. A body that scrolls above its own controls holds its scroll to it (New session). Read it in the JSX.
 */
export const SheetRoom = context<() => number>(() => Number.POSITIVE_INFINITY);

/**
 * The Soft sheet (soft-final/SPEC.md §11 Sheet): rises from the bottom on the sheet
 * canvas with 28 pt top corners, at most 88 % of the height, over the scrim; a drawn
 * 36 × 5 grabber 8 pt from the top; an optional title row (20/700, padding 11/16/19).
 * It closes on the scrim, Back and a swipe. Groups inside use `bg-sheet-surface`.
 * With the keyboard up in a short window (a phone held sideways) a bare title row gives its room to the body.
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
  // A row holding a Back or a Close stays: only a bare title gives way.
  const titled = () => Boolean(props.leading || props.trailing) || (Boolean(title()) && !sheetTitleGivesWay(windowSize().height, keyboardFrame().height));
  // The grabber (the dialog's top padding on wide) and the title row as laid out.
  const header = signal(0);
  // On wide a centred dialog over the keyboard (shell/AppSheet.tsx), with its own cap and foot.
  const room = () => (wide()
    ? dialogBodyRoom({ window: windowSize().height, keyboard: keyboardFrame().height, header: header(), insets: dialogInsets() })
    : sheetBodyRoom({ window: windowSize().height, keyboard: keyboardFrame().height, header: header() }));
  return (
    <AppSheet open={props.open()} onOpenChange={({ open }) => !open && props.onClose()} closeOnSwipe
      popupClass="max-h-[88%] rounded-t-(--radius-sheet) bg-sheet shadow-float" backdropClass="bg-scrim">
      <View testID={props.testID} style={{ paddingBottom: wide() ? DIALOG_FOOT : sheetFoot(keyboardFrame().height) }}>
        {/* The header as laid out, the dialog's top padding with it: the room counts all of it. */}
        <View class={wide() ? "pt-3" : ""} onLayout={({ bounds }) => header.set(bounds.size.height)}>
          {/* A dialog on wide has no grabber (SPEC §14.2). */}
          <If when={!wide()}><View class="items-center pt-2">
              <View class="h-[5px] w-[36px] rounded-full bg-track" />
            </View></If>
          <If when={titled()} else={<View class="h-4" />}><View class="min-h-[44px] flex-row items-center gap-2 px-4 pb-[19px] pt-[11px]">
              {props.leading ?? null}
              <Text class="flex-1 text-[20px] font-bold text-foreground line-clamp-1">{title()}</Text>
              {props.trailing ?? null}
            </View></If>
        </View>
        <SheetRoom.Provider value={room}>{props.children ?? null}</SheetRoom.Provider>
      </View>
    </AppSheet>
  );
}
