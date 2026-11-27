import { keymap, PanResponder, untracked, View, type ViewRef } from "nsh";
import { PREVIEW_SPLIT, shareOf } from "../../core/web/beside";
import { previewShare, previewWidths, setPreviewShare, splitRoom } from "./layout";

/**
 * The handle between Brain's thread and a laptop's preview (client/DESIGN.md "The wide layout", Beside): drag it, or
 * ← and → in steps of 16 while it has the focus, to give the preview more or less of the pane, within the thread's
 * 480 and the preview's 540; double-click or double-tap gives the preview its 55 % again. The share is kept on this
 * device, so the next preview opens as wide, in a window of any size. Pointer and touch alike, as the left pane's.
 */
export function SplitHandle() {
  let start = 0;
  let lastTap = 0;
  let node: ViewRef | null = null;
  const to = (width: number) => shareOf(untracked(splitRoom), width);
  const drag = PanResponder.create({
    onStartShouldSetPanResponder: () => true,
    onPanResponderGrant: () => {
      start = untracked(previewWidths).preview;
      node?.focus();
    },
    // The preview is at the right: dragging left widens it.
    onPanResponderMove: (_e, g) => previewShare.set(to(start - g.dx)),
    onPanResponderRelease: (_e, g) => {
      if (Math.abs(g.dx) < 3 && Math.abs(g.dy) < 3) {
        const now = Date.now();
        if (now - lastTap < 400) {
          lastTap = 0;
          return setPreviewShare(PREVIEW_SPLIT.share);
        }
        lastTap = now;
      }
      setPreviewShare(untracked(previewShare));
    },
    onPanResponderTerminate: () => setPreviewShare(untracked(previewShare)),
  });
  const step = (direction: -1 | 1) => setPreviewShare(to(untracked(previewWidths).preview - direction * PREVIEW_SPLIT.step));
  const bindArrows = () => keymap.bind({ left: () => step(-1), right: () => step(1) });
  let unbind: (() => void) | null = null;
  return (
    <View {...drag.panHandlers} ref={(ref) => void (node = ref)} testID="split-handle" role="adjustable" accessibilityLabel="Resize the preview" focusable
      accessibilityValue={{ min: PREVIEW_SPLIT.preview, max: Math.max(PREVIEW_SPLIT.preview, splitRoom() - PREVIEW_SPLIT.handle - PREVIEW_SPLIT.thread), now: previewWidths().preview }}
      onPress={(e) => {
        if (e.clickCount === 2) setPreviewShare(PREVIEW_SPLIT.share);
      }}
      onFocus={() => {
        unbind?.();
        unbind = bindArrows();
      }}
      onBlur={() => {
        unbind?.();
        unbind = null;
      }}
      class="group h-full items-center bg-background focus-visible:bg-accent" style={{ width: PREVIEW_SPLIT.handle, cursor: "col-resize" }}>
      <View class="h-full w-px bg-border group-hover:bg-ring" />
    </View>
  );
}
