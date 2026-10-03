/**
 * UI-thread state for the primary drawer.
 *
 * `target` is the single owner of whether the drawer is open. Gesture
 * release, backdrop taps and every JS request (menu, close button, Back,
 * navigation) write it on the UI thread; React only mirrors it through
 * ordered `notify` calls. A request therefore always acts on what the user
 * sees, even when JS is behind the UI thread. Positions are normalized:
 * 0 is closed and 1 is fully open.
 */

export interface DrawerCell<T> {
  value: T;
}

export interface PrimaryDrawerMotion {
  /** Rendered position, 0 closed … 1 open. Drives every visual. */
  position: DrawerCell<number>;
  /** Where the drawer is settling: 0 or 1. The source of truth. */
  target: DrawerCell<number>;
  /** True while a pan owns `position`. */
  dragging: DrawerCell<boolean>;
  dragStart: DrawerCell<number>;
  /** A request (0 or 1) that arrived during a drag; -1 when none. */
  pending: DrawerCell<number>;
}

export interface PrimaryDrawerEffects {
  /** Animate `position` toward `target`, seeding the normalized velocity. */
  settle(target: number, velocity: number): void;
  /** Stop any settle in flight so the finger owns `position`. */
  hold(): void;
  /** Report a target change to React. Called only when the target changes. */
  notify(target: number): void;
}

const NO_REQUEST = -1;
/** Release thresholds, matching react-native-drawer-layout. */
export const DRAWER_SWIPE_MIN_OFFSET = 5;
export const DRAWER_SWIPE_MIN_DISTANCE = 60;
export const DRAWER_SWIPE_MIN_VELOCITY = 500;

function clamp(value: number, min: number, max: number): number {
  "worklet";
  return Math.min(Math.max(value, min), max);
}

function commitDrawerTarget(
  motion: PrimaryDrawerMotion,
  target: number,
  velocity: number,
  effects: PrimaryDrawerEffects,
): void {
  "worklet";
  const changed = motion.target.value !== target;
  motion.target.value = target;
  effects.settle(target, velocity);
  if (changed) {
    effects.notify(target);
  }
}

/**
 * Apply an open/close intent. During a drag the finger owns the drawer, so
 * the intent waits for release and then wins over the gesture's own result.
 */
export function requestDrawerTarget(
  motion: PrimaryDrawerMotion,
  open: boolean,
  effects: PrimaryDrawerEffects,
): void {
  "worklet";
  if (motion.dragging.value) {
    motion.pending.value = open ? 1 : 0;
    return;
  }
  commitDrawerTarget(motion, open ? 1 : 0, 0, effects);
}

export function beginDrawerDrag(
  motion: PrimaryDrawerMotion,
  effects: PrimaryDrawerEffects,
): void {
  "worklet";
  // Without this, a settle still running at activation keeps moving the
  // drawer until the first update, which then snaps it back to `dragStart`.
  effects.hold();
  motion.dragging.value = true;
  motion.dragStart.value = motion.position.value;
  motion.pending.value = NO_REQUEST;
}

/** `offsetX` is the finger travel in points, positive toward open. */
export function updateDrawerDrag(
  motion: PrimaryDrawerMotion,
  offsetX: number,
  drawerWidth: number,
): void {
  "worklet";
  if (!motion.dragging.value || drawerWidth <= 0) {
    return;
  }
  motion.position.value = clamp(
    motion.dragStart.value + offsetX / drawerWidth,
    0,
    1,
  );
}

/**
 * Where a released drag settles. A short or slow pan returns to the target
 * the drawer had before the drag. A fling settles in its direction; any
 * other decisive drag settles where the finger travelled, so a slow drift
 * at release cannot undo a long drag.
 */
export function resolveDrawerRelease(
  currentTarget: number,
  translationX: number,
  velocityX: number,
): number {
  "worklet";
  const decisive =
    (Math.abs(translationX) > DRAWER_SWIPE_MIN_OFFSET &&
      Math.abs(velocityX) > DRAWER_SWIPE_MIN_VELOCITY) ||
    Math.abs(translationX) > DRAWER_SWIPE_MIN_DISTANCE;
  if (!decisive) {
    return currentTarget;
  }
  const fling = Math.abs(velocityX) > DRAWER_SWIPE_MIN_VELOCITY;
  return (fling ? velocityX : translationX) > 0 ? 1 : 0;
}

/**
 * Finish a drag, including cancelled and interrupted ones: the drawer always
 * settles to a definite target and React hears about any change. A drag the
 * system cancelled (`completed` false) was not a decision, so the drawer
 * returns to its previous target unless an explicit request arrived meanwhile.
 * Touches that never activated a drag change nothing.
 */
export function endDrawerDrag(
  motion: PrimaryDrawerMotion,
  translationX: number,
  velocityX: number,
  drawerWidth: number,
  completed: boolean,
  effects: PrimaryDrawerEffects,
): void {
  "worklet";
  if (!motion.dragging.value) {
    return;
  }
  motion.dragging.value = false;
  const pending = motion.pending.value;
  motion.pending.value = NO_REQUEST;
  if (pending !== NO_REQUEST) {
    commitDrawerTarget(motion, pending, 0, effects);
    return;
  }
  if (!completed) {
    commitDrawerTarget(motion, motion.target.value, 0, effects);
    return;
  }
  const next = resolveDrawerRelease(motion.target.value, translationX, velocityX);
  commitDrawerTarget(
    motion,
    next,
    drawerWidth > 0 ? velocityX / drawerWidth : 0,
    effects,
  );
}
