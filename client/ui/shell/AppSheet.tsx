import { Dialog, Sheet, type SheetProps } from "nsh/base";
import { isAvailable as mobile } from "nsh/mobile";
import { desktop } from "./Shell";

/** The card a sheet becomes on the desktop: centered near the top, one inset all round (app/DESIGN.md). */
const DIALOG = "mt-24 w-[560px] max-w-[92%] self-center overflow-hidden rounded-xl border border-border";

/**
 * A bottom sheet on phones and narrow windows; on the desktop layout a centered
 * dialog with the same content, without the grabber's rounded top. Escape and
 * the backdrop close either.
 */
export function AppSheet(props: SheetProps & { testID?: string }) {
  if (mobile) return <Sheet {...props} />;
  const popup = () => `${DIALOG} ${(props.popupClass ?? "").replace(/\brounded-t-\S+/g, "")}`;
  return (
    <>
      {desktop() ? (
        <Dialog testID={props.testID} open={props.open ?? false} animation="fade" class={popup()} backdropClass={props.backdropClass ?? "bg-black/30"}
          closeOnEscape={props.closeOnEscape ?? true} closeOnBackdropPress={props.closeOnBackdropPress ?? true}
          onOpenChange={({ open, reason }) => props.onOpenChange?.({ open, reason })} onCancel={props.onCancel} onClose={props.onClose}>
          {props.children}
        </Dialog>
      ) : <Sheet {...props} />}
    </>
  );
}
