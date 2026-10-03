import { describe, expect, test } from "bun:test";
import {
  beginDrawerDrag,
  endDrawerDrag,
  requestDrawerTarget,
  resolveDrawerRelease,
  updateDrawerDrag,
  type PrimaryDrawerEffects,
  type PrimaryDrawerMotion,
} from "./primaryDrawerMotion";

const WIDTH = 320;

/**
 * Two-thread harness. UI work runs immediately or from `ui` queue; anything
 * the UI thread sends to JS (notify) waits in the `js` queue, which is how
 * runOnJS behaves while JS is busy. React's mirror is updated only from
 * notifications, exactly like the shell. `position` behaves like a
 * Reanimated shared value: a settle animates it frame by frame until it
 * completes, and any direct write cancels the settle.
 */
function harness() {
  let rendered = 0;
  let spring: { target: number } | null = null;
  const motion: PrimaryDrawerMotion = {
    position: {
      get value() {
        return rendered;
      },
      set value(next: number) {
        spring = null;
        rendered = next;
      },
    },
    target: { value: 0 },
    dragging: { value: false },
    dragStart: { value: 0 },
    pending: { value: -1 },
  };
  const ui: Array<() => void> = [];
  const js: Array<() => void> = [];
  const mirror = { open: false };
  const notified: number[] = [];
  const effects: PrimaryDrawerEffects = {
    settle(target) {
      const current = { target };
      spring = current;
      // Queued completion: the spring reaches its target unless cancelled.
      ui.push(() => {
        if (spring === current) {
          rendered = target;
          spring = null;
        }
      });
    },
    hold() {
      spring = null;
    },
    notify(target) {
      notified.push(target);
      js.push(() => {
        mirror.open = target === 1;
      });
    },
  };
  const runUi = () => {
    while (ui.length) ui.shift()!();
  };
  const runJs = () => {
    while (js.length) js.shift()!();
  };
  return {
    motion,
    mirror,
    notified,
    /** One spring frame: halfway to the target. */
    frame() {
      if (spring) rendered += (spring.target - rendered) / 2;
    },
    /** A drag the system cancels after it became active. */
    cancelledDrag(dx: number, velocityX: number, request?: boolean) {
      beginDrawerDrag(motion, effects);
      updateDrawerDrag(motion, dx, WIDTH);
      if (request !== undefined) requestDrawerTarget(motion, request, effects);
      endDrawerDrag(motion, dx, velocityX, WIDTH, false, effects);
    },
    /** A JS request (menu, close button, Back) hops to the UI thread. */
    request(open: boolean) {
      ui.push(() => requestDrawerTarget(motion, open, effects));
    },
    swipe(dx: number, velocityX: number) {
      beginDrawerDrag(motion, effects);
      updateDrawerDrag(motion, dx, WIDTH);
      endDrawerDrag(motion, dx, velocityX, WIDTH, true, effects);
    },
    effects,
    runUi,
    runJs,
    flush() {
      runUi();
      runJs();
    },
  };
}

/**
 * Model of the react-native-drawer-layout 4.2.4 contract this shell used:
 * the `open` prop is the only path to the animation and gesture results
 * reach JS through a queued onOpen/onClose.
 */
function legacyDrawerModel() {
  let uiTarget = 0;
  let committedOpen = false;
  const js: Array<() => void> = [];
  const commit = (next: boolean) => {
    if (next === committedOpen) return; // React bails out: no prop change.
    committedOpen = next;
    uiTarget = next ? 1 : 0; // useEffect(() => toggleDrawer(open), [open])
  };
  return {
    gestureOpens() {
      uiTarget = 1;
      js.push(() => commit(true)); // runOnJS(onOpen)
    },
    closeButton() {
      commit(false);
    },
    runJs() {
      while (js.length) js.shift()!();
    },
    get uiTarget() {
      return uiTarget;
    },
    get committedOpen() {
      return committedOpen;
    },
  };
}

describe("primary drawer motion ownership", () => {
  test("legacy prop-only contract loses a close tapped before onOpen reaches JS", () => {
    // Device order captured on Android with a busy JS thread: the close
    // press was handled before the gesture's onOpen.
    const legacy = legacyDrawerModel();
    legacy.gestureOpens();
    legacy.closeButton();
    legacy.runJs();
    expect(legacy.uiTarget).toBe(1);
    expect(legacy.committedOpen).toBe(true);
  });

  test("close tapped before the swipe-open notification still closes", () => {
    const h = harness();
    h.swipe(WIDTH, 1200); // UI decides open; notify(1) queued for JS.
    expect(h.motion.target.value).toBe(1);
    expect(h.mirror.open).toBe(false);
    h.request(false); // JS handles the close press first.
    h.runUi();
    h.runJs(); // Stale notify(1), then notify(0), arrive in order.
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);
  });

  test("open then close in one JS batch never leaves a visible drawer", () => {
    const h = harness();
    h.request(true);
    h.request(false);
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);
  });

  test("close during the opening spring settles closed", () => {
    const h = harness();
    h.request(true);
    // Apply the request but not the spring's settle frame.
    h.runUi();
    h.motion.position.value = 0.4;
    h.request(false);
    h.flush();
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);
  });

  test("navigation dismiss while a swipe-open is still unreported closes", () => {
    const h = harness();
    h.swipe(WIDTH * 0.8, 900);
    h.request(false);
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.mirror.open).toBe(false);
  });

  test("fast reversal follows the last fling", () => {
    const h = harness();
    h.swipe(WIDTH * 0.7, 800);
    h.runUi();
    h.motion.position.value = 0.9; // Still settling when the finger returns.
    h.swipe(-WIDTH * 0.3, -1400);
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);
  });

  test("a short or slow drag returns to where it started", () => {
    const h = harness();
    h.request(true);
    h.flush();
    h.swipe(-30, -80);
    h.flush();
    expect(h.motion.target.value).toBe(1);
    expect(h.motion.position.value).toBe(1);
    expect(h.mirror.open).toBe(true);
  });

  test("a request during a drag waits for release and then wins", () => {
    const h = harness();
    beginDrawerDrag(h.motion, h.effects);
    updateDrawerDrag(h.motion, WIDTH * 0.6, WIDTH);
    requestDrawerTarget(h.motion, false, h.effects); // Back while dragging.
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBeCloseTo(0.6);
    endDrawerDrag(h.motion, WIDTH * 0.6, 900, WIDTH, true, h.effects);
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);
  });

  test("repeated open/close requests keep mirror and target in step", () => {
    const h = harness();
    for (let i = 0; i < 25; i += 1) {
      h.request(i % 2 === 0);
      if (i % 3 === 0) h.flush();
      else h.runUi();
    }
    h.flush();
    expect(h.motion.target.value).toBe(1);
    expect(h.mirror.open).toBe(true);
    expect(h.motion.position.value).toBe(1);
  });

  test("drag clamps to the drawer and ignores updates after release", () => {
    const h = harness();
    beginDrawerDrag(h.motion, h.effects);
    updateDrawerDrag(h.motion, WIDTH * 3, WIDTH);
    expect(h.motion.position.value).toBe(1);
    updateDrawerDrag(h.motion, -WIDTH * 3, WIDTH);
    expect(h.motion.position.value).toBe(0);
    endDrawerDrag(h.motion, -WIDTH * 3, -900, WIDTH, true, h.effects);
    updateDrawerDrag(h.motion, WIDTH, WIDTH);
    expect(h.motion.position.value).toBe(0);
  });

  test("a drag that interrupts a settle owns the drawer from activation", () => {
    const h = harness();
    requestDrawerTarget(h.motion, true, h.effects); // Opening spring starts.
    h.frame();
    h.frame();
    expect(h.motion.position.value).toBe(0.75);
    beginDrawerDrag(h.motion, h.effects);
    h.frame(); // A frame lands before the first pan update.
    expect(h.motion.position.value).toBe(0.75);
    updateDrawerDrag(h.motion, 0, WIDTH);
    expect(h.motion.position.value).toBe(0.75);
    h.runUi(); // The interrupted spring never completes over the finger.
    expect(h.motion.position.value).toBe(0.75);
  });

  test("release thresholds match the previous drawer", () => {
    expect(resolveDrawerRelease(0, 40, 200)).toBe(0);
    expect(resolveDrawerRelease(0, 61, 0)).toBe(1);
    expect(resolveDrawerRelease(0, 10, 600)).toBe(1);
    expect(resolveDrawerRelease(1, -10, -600)).toBe(0);
    expect(resolveDrawerRelease(1, 80, -700)).toBe(0);
    expect(resolveDrawerRelease(1, 4, -2000)).toBe(1);
  });

  test("a long drag is not undone by a slow drift at release", () => {
    // Velocities recorded on Android: a 114dp closing drag released with a
    // small rightward velocity reopened the drawer under the old rule.
    expect(resolveDrawerRelease(1, -114, 366)).toBe(0);
    expect(resolveDrawerRelease(0, 90, -120)).toBe(1);
    // A real fling back still wins.
    expect(resolveDrawerRelease(1, -114, 900)).toBe(1);
  });
});

describe("primary drawer cancellation", () => {
  test("a long cancelled opening drag stays closed", () => {
    const h = harness();
    h.cancelledDrag(WIDTH * 0.8, 1500);
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);
    expect(h.notified).toEqual([]);
  });

  test("a long cancelled closing drag stays open", () => {
    const h = harness();
    h.request(true);
    h.flush();
    h.cancelledDrag(-WIDTH * 0.8, -1500);
    h.flush();
    expect(h.motion.target.value).toBe(1);
    expect(h.motion.position.value).toBe(1);
    expect(h.mirror.open).toBe(true);
    expect(h.notified).toEqual([1]);
  });

  test("an explicit request during a cancelled drag still wins", () => {
    const h = harness();
    h.request(true);
    h.flush();
    h.cancelledDrag(-WIDTH * 0.1, 0, false); // Back or navigation mid-drag.
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.mirror.open).toBe(false);

    h.cancelledDrag(WIDTH * 0.1, 0, true);
    h.flush();
    expect(h.motion.target.value).toBe(1);
    expect(h.mirror.open).toBe(true);
  });

  test("the next gesture after a cancellation works normally", () => {
    const h = harness();
    h.cancelledDrag(WIDTH * 0.8, 1500);
    h.flush();
    h.swipe(WIDTH * 0.8, 1200);
    h.flush();
    expect(h.motion.target.value).toBe(1);
    expect(h.motion.position.value).toBe(1);
    expect(h.mirror.open).toBe(true);
  });

  test("touches that never activated a drag change nothing", () => {
    const h = harness();
    endDrawerDrag(h.motion, WIDTH, 2000, WIDTH, false, h.effects);
    endDrawerDrag(h.motion, WIDTH, 2000, WIDTH, true, h.effects);
    h.flush();
    expect(h.motion.target.value).toBe(0);
    expect(h.motion.position.value).toBe(0);
    expect(h.notified).toEqual([]);
  });
});
