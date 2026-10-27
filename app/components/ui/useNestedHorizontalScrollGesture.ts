import { useMemo } from "react";
import { Gesture } from "react-native-gesture-handler";

/**
 * Wrap a horizontal ScrollView that sits inside the primary drawer or a
 * pager (a table, a code block) in a GestureDetector with this gesture.
 *
 * The pan claims a horizontal drag at 6pt, before the drawer (12pt) and the
 * pager, and runs alongside the ScrollView's own native gesture, so the
 * content scrolls and nothing outside moves. On Android the native scroller
 * would win anyway; on iOS a UIScrollView does not cancel RNGH's pans by
 * itself, so without this the drawer could open while the content scrolls.
 * Vertical drags fail it at 8pt and stay with the list.
 */
export function useNestedHorizontalScrollGesture() {
  return useMemo(
    () =>
      Gesture.Simultaneous(
        Gesture.Pan().activeOffsetX([-6, 6]).failOffsetY([-8, 8]),
        Gesture.Native(),
      ),
    [],
  );
}
