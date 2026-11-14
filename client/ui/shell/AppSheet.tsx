import { cleanup, effect, Keyboard, keyboardFrame, platform, safeArea, signal, split, untracked, windowSize } from "nsh";
import { Dialog, Sheet, type DialogProps, type SheetProps } from "nsh/base";
import { sheetOpened, switching } from "../session";
import { dialogFrame } from "../../core/shell/sheet";
import { wide } from "./layout";

/**
 * Whether a sheet may show: a switch of computer closes it first (NSH 0.4.0 crashes
 * when the page under an open sheet goes) and tells its owner, and it stays closed
 * until the owner opens it again.
 */
function guardedOpen(props: { open?: boolean | null; onOpenChange?: (event: { open: boolean; reason: "cancel" }) => void }): () => boolean {
  const dismissed = signal(false);
  let closed: (() => void) | null = null;
  effect(() => {
    const open = Boolean(props.open);
    if (open && !closed) closed = sheetOpened();
    if (!open) {
      // A sheet that closes takes the keyboard its field raised with it (R4#4: it stayed up over a page with no field).
      if (closed) Keyboard.dismiss();
      closed?.();
      closed = null;
      dismissed.set(false);
    }
  });
  effect(() => {
    if (!switching() || !untracked(() => Boolean(props.open))) return;
    dismissed.set(true);
    Keyboard.dismiss();
    closed?.();
    closed = null;
    props.onOpenChange?.({ open: false, reason: "cancel" });
  });
  cleanup(() => closed?.());
  return () => Boolean(props.open) && !dismissed();
}

/**
 * The panel's style, lifted over the software keyboard and its content clear of the screen's sides. NSH's bottom sheet does not move for it, so a
 * sheet's field and its buttons (Commit, Ask the agent, a branch, a search) sat behind the keyboard.
 * Read in the JSX, so it follows the keyboard.
 */
function aboveKeyboard(props: SheetProps & SideProps): SheetProps["popupStyle"] {
  if ((props.side ?? "bottom") !== "bottom") return props.popupStyle;
  const sides = sheetSides(props.sidePadding);
  const height = keyboardFrame().height;
  return [props.popupStyle, height > 0 ? { ...sides, paddingBottom: height + 16 } : sides];
}

/** A bottom sheet's own space at its sides (points), which its class must not set: the screen's side insets are added to it. */
export type SideProps = { sidePadding?: number };

/**
 * A bottom sheet's side padding: its own, plus what the system holds at the window's sides (a phone held sideways,
 * the iPhone Duo's 84 pt bar). The panel is drawn over the whole window, so these are the window's insets
 * (`safeArea()`), never `env(safe-area-inset-*)`: env() reads the insets of the page the sheet is declared in, and a
 * page is already clear of the bar (the shell pads its row), so it read 0 there. Rename's Save and Brain's menu sat
 * under the bar while New session, declared outside the stack, kept clear of it (Mac re-run 11-14, 3c).
 * Read in the JSX, so it follows a turn.
 */
export function sheetSides(padding = 0): { paddingLeft: number; paddingRight: number } {
  return { paddingLeft: padding + safeArea().left, paddingRight: padding + safeArea().right };
}

/** NSH's Dialog (a page sheet on iOS), closed by a switch of computer like AppSheet. */
export function GuardedDialog(props: DialogProps) {
  const open = guardedOpen(props);
  return <Dialog {...props} class={`${INK} ${props.class ?? ""}`} open={open()} />;
}

/** NSH's bottom sheet, closed by a switch of computer like AppSheet. */
export function GuardedSheet(props: SheetProps & SideProps) {
  const open = guardedOpen(props);
  const [, sheet] = split(props, ["sidePadding"]);
  return <Sheet {...sheet} popupClass={`${INK} ${props.popupClass ?? ""}`} popupStyle={aboveKeyboard(props)} open={open()} />;
}

/** The theme's ink for a sheet's or dialog's texts that say no color of their own (NSH draws them black; unreadable in Dark). */
const INK = "text-foreground";

/**
 * The dialog a sheet becomes on wide (soft-final SPEC §14.2): 480 wide, centred, radius 28, over the
 * same scrim, no grabber. iOS shows it as its own centred form sheet (a dialog there hides what is
 * behind unless it is a sheet), so the card fills that.
 */
const DIALOG = platform.os === "ios"
  ? "flex-1 overflow-hidden bg-sheet"
  : "w-[480px] max-w-[92%] max-h-[88%] self-center my-auto overflow-hidden rounded-[28px] bg-sheet shadow-float";

/**
 * The window's top and bottom safe areas, which a dialog stays inside of (core/shell/sheet.ts `dialogFrame`). iOS
 * presents it as its own form sheet, sized by the system: none there.
 */
export function dialogInsets(): number {
  if (platform.os === "ios") return 0;
  const area = safeArea();
  return area.top + area.bottom;
}

/**
 * The dialog's frame (core/shell/sheet.ts `dialogFrame`): held to the height its body's room is measured against
 * (kit/Sheet.tsx), and over the software keyboard centred in what the keyboard leaves. iOS presents the dialog as its
 * own form sheet and keeps it clear of the keyboard itself. Read in the JSX, so it follows the keyboard.
 */
function dialogStyle(sidePadding = 0) {
  // The sheet's own side space; a dialog stands clear of the screen's sides already.
  const sides = sidePadding ? { paddingLeft: sidePadding, paddingRight: sidePadding } : undefined;
  if (platform.os === "ios") return sides;
  const frame = dialogFrame(windowSize().height, keyboardFrame().height, dialogInsets());
  return frame.lift > 0 ? { ...sides, marginBottom: frame.lift, maxHeight: frame.max } : { ...sides, maxHeight: frame.max };
}

/**
 * A bottom sheet below the wide gate; on wide, on every device, a centred dialog with the same content,
 * without the grabber's rounded top, above the keyboard. Escape and the scrim close either. Crossing the gate
 * (a resize, a turn, a fold) with one open shows it the other way.
 */
export function AppSheet(props: SheetProps & SideProps & { testID?: string }) {
  const open = guardedOpen(props);
  const popup = () => `${DIALOG} ${INK} ${(props.popupClass ?? "").replace(/\brounded-t-\S+|\bmax-h-\S+|\bbg-\S+/g, "")}`;
  const [, sheet] = split(props, ["sidePadding"]);
  return (
    <>
      <If when={wide()} else={<Sheet {...sheet} popupClass={`${INK} ${props.popupClass ?? ""}`} popupStyle={aboveKeyboard(props)} open={open()} />}><Dialog testID={props.testID} open={open()} animation="fade" class={popup()} style={dialogStyle(props.sidePadding)} backdropClass={platform.os === "ios" ? undefined : "bg-scrim"}
          presentation={platform.os === "ios" ? "formSheet" : undefined}
          closeOnEscape={props.closeOnEscape ?? true} closeOnBackdropPress={props.closeOnBackdropPress ?? true}
          onOpenChange={({ open, reason }) => props.onOpenChange?.({ open, reason })} onCancel={props.onCancel} onClose={props.onClose}>
          {props.children}
        </Dialog></If>
    </>
  );
}
