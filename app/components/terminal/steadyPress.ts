/**
 * A press that lets the finger drift. Pressable decides on every move whether
 * the finger is still over the view, by the rect it measured when the touch
 * began. Inside the inverted timeline that measure reports where the layout
 * puts the view, mirrored from where it is drawn, so the first jitter of a real
 * finger reads as "left the button" and the press is cancelled; a tap that
 * never moves (adb's, a stylus') still lands. This measures nothing: a press
 * commits on release unless the finger travelled farther than the touch slop
 * from where it landed, or the system took the touch for a scroll.
 */
export const STEADY_PRESS_SLOP = 10;

/** Where a press began, in the window and inside the view it began on. */
export interface SteadyTouch {
  pageX: number;
  pageY: number;
  locationX: number;
  locationY: number;
}

export interface SteadyPressEvent {
  nativeEvent: SteadyTouch;
}

export interface SteadyPress {
  grant(event: SteadyPressEvent): void;
  move(event: SteadyPressEvent): void;
  release(): void;
  terminate(): void;
  /** A press that has drifted may be taken by the scroll view; a steady one holds on. */
  canTerminate(): boolean;
}

export function createSteadyPress({
  onPress,
  onActiveChange,
  slop = STEADY_PRESS_SLOP,
}: {
  onPress(touch: SteadyTouch): void;
  onActiveChange(active: boolean): void;
  slop?: number;
}): SteadyPress {
  let start: SteadyTouch | null = null;
  let moved = false;

  const finish = () => {
    start = null;
    moved = false;
    onActiveChange(false);
  };

  return {
    grant(event) {
      const { pageX, pageY, locationX, locationY } = event.nativeEvent;
      start = { pageX, pageY, locationX, locationY };
      moved = false;
      onActiveChange(true);
    },
    move(event) {
      if (!start || moved) return;
      const { pageX, pageY } = event.nativeEvent;
      if (Math.abs(pageX - start.pageX) > slop || Math.abs(pageY - start.pageY) > slop) {
        moved = true;
        onActiveChange(false);
      }
    },
    release() {
      const touch = start;
      const commit = touch !== null && !moved;
      finish();
      if (commit && touch) onPress(touch);
    },
    terminate: finish,
    canTerminate: () => moved,
  };
}
