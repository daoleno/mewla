import { cleanup, effect, Keyboard, keyboardFrame, platform, signal, untracked } from "nsh";
import { Dialog, Sheet, type DialogProps, type SheetProps } from "nsh/base";
import { sheetOpened, switching } from "../session";
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
 * The panel's style, lifted over the software keyboard. NSH's bottom sheet does not move for it, so a
 * sheet's field and its buttons (Commit, Ask the agent, a branch, a search) sat behind the keyboard.
 * Read in the JSX, so it follows the keyboard.
 */
function aboveKeyboard(props: SheetProps): SheetProps["popupStyle"] {
  const height = keyboardFrame().height;
  if (height <= 0 || (props.side ?? "bottom") !== "bottom") return props.popupStyle;
  return [props.popupStyle, { paddingBottom: height + 16 }];
}

/** NSH's Dialog (a page sheet on iOS), closed by a switch of computer like AppSheet. */
export function GuardedDialog(props: DialogProps) {
  const open = guardedOpen(props);
  return <Dialog {...props} class={`${INK} ${props.class ?? ""}`} open={open()} />;
}

/** NSH's bottom sheet, closed by a switch of computer like AppSheet. */
export function GuardedSheet(props: SheetProps) {
  const open = guardedOpen(props);
  return <Sheet {...props} popupClass={`${INK} ${props.popupClass ?? ""}`} popupStyle={aboveKeyboard(props)} open={open()} />;
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
 * A bottom sheet below 1024 pt; on wide, on every device, a centred dialog with the same content,
 * without the grabber's rounded top. Escape and the scrim close either. Crossing 1024 with one open
 * shows it the other way.
 */
export function AppSheet(props: SheetProps & { testID?: string }) {
  const open = guardedOpen(props);
  const popup = () => `${DIALOG} ${INK} ${(props.popupClass ?? "").replace(/\brounded-t-\S+|\bmax-h-\S+|\bbg-\S+/g, "")}`;
  return (
    <>
      <If when={wide()} else={<Sheet {...props} popupClass={`${INK} ${props.popupClass ?? ""}`} popupStyle={aboveKeyboard(props)} open={open()} />}><Dialog testID={props.testID} open={open()} animation="fade" class={popup()} backdropClass={platform.os === "ios" ? undefined : "bg-scrim"}
          presentation={platform.os === "ios" ? "formSheet" : undefined}
          closeOnEscape={props.closeOnEscape ?? true} closeOnBackdropPress={props.closeOnBackdropPress ?? true}
          onOpenChange={({ open, reason }) => props.onOpenChange?.({ open, reason })} onCancel={props.onCancel} onClose={props.onClose}>
          {props.children}
        </Dialog></If>
    </>
  );
}
