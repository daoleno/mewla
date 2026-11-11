import { cleanup, effect, signal, untracked } from "nsh";
import { Dialog, Sheet, type DialogProps, type SheetProps } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { sheetOpened, switching } from "../session";
import { desktop } from "./Shell";

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
      closed?.();
      closed = null;
      dismissed.set(false);
    }
  });
  effect(() => {
    if (!switching() || !untracked(() => Boolean(props.open))) return;
    dismissed.set(true);
    closed?.();
    closed = null;
    props.onOpenChange?.({ open: false, reason: "cancel" });
  });
  cleanup(() => closed?.());
  return () => Boolean(props.open) && !dismissed();
}

/** NSH's Dialog (a page sheet on iOS), closed by a switch of computer like AppSheet. */
export function GuardedDialog(props: DialogProps) {
  const open = guardedOpen(props);
  return <Dialog {...props} class={`${INK} ${props.class ?? ""}`} open={open()} />;
}

/** NSH's bottom sheet, closed by a switch of computer like AppSheet. */
export function GuardedSheet(props: SheetProps) {
  const open = guardedOpen(props);
  return <Sheet {...props} popupClass={`${INK} ${props.popupClass ?? ""}`} open={open()} />;
}

/** The theme's ink for a sheet's or dialog's texts that say no color of their own (NSH draws them black; unreadable in Dark). */
const INK = "text-foreground";

/** The card a sheet becomes on the desktop: centered near the top, one inset all round (app/DESIGN.md). */
const DIALOG = "mt-24 w-[560px] max-w-[92%] self-center overflow-hidden rounded-xl border border-border";

/**
 * A bottom sheet on phones and narrow windows; on the desktop layout a centered
 * dialog with the same content, without the grabber's rounded top. Escape and
 * the backdrop close either.
 */
export function AppSheet(props: SheetProps & { testID?: string }) {
  const open = guardedOpen(props);
  if (mobile) return <Sheet {...props} popupClass={`${INK} ${props.popupClass ?? ""}`} open={open()} />;
  const popup = () => `${DIALOG} ${INK} ${(props.popupClass ?? "").replace(/\brounded-t-\S+/g, "")}`;
  return (
    <>
      {desktop() ? (
        <Dialog testID={props.testID} open={open()} animation="fade" class={popup()} backdropClass={props.backdropClass ?? "bg-black/30"}
          closeOnEscape={props.closeOnEscape ?? true} closeOnBackdropPress={props.closeOnBackdropPress ?? true}
          onOpenChange={({ open, reason }) => props.onOpenChange?.({ open, reason })} onCancel={props.onCancel} onClose={props.onClose}>
          {props.children}
        </Dialog>
      ) : <Sheet {...props} popupClass={`${INK} ${props.popupClass ?? ""}`} open={open()} />}
    </>
  );
}
