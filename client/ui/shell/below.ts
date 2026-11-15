import { computed, safeArea, signal, windowSize } from "nsh";
import { belowStack } from "../../core/shell/belowStack";

/** The stack pane's bottom as last laid out (`Shell` sets it); none before its first layout. */
export const paneBottom = signal<number | null>(null);

/**
 * What lies below the stack (the system bar): the keyboard covers it before it reaches a page. Read from the
 * window and the pane as they are now (core/shell/belowStack.ts): a rotation changes both, in either order.
 */
export const belowSections = computed(() => belowStack(windowSize().height, paneBottom()));

/**
 * The system's bottom bar (Android's navigation bar, the iPhone's home indicator) where it lies over the stack:
 * a page's foot is lifted by it so nothing at the bottom hides under the bar.
 */
export function bottomBarInset(): number {
  return Math.max(0, safeArea().bottom - belowSections());
}
