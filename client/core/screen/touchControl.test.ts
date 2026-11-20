import { describe, expect, test } from "bun:test";
import { TOUCH, TouchControl, type TouchAction } from "./touchControl";

// A 1280×800 desktop in a 390×720 view: fit is 0.3047, the picture sits at ty 238 (as in the measured runs).
function desk(options: { fineScroll?: boolean; handle?: boolean } = {}) {
  const c = new TouchControl(options);
  c.viewport(390, 720);
  c.picture(1280, 800);
  return c;
}

/** Drives one control with a clock: fingers in, actions out. */
function driver(c: TouchControl) {
  let t = 1000;
  const out: TouchAction[] = [];
  const run = (until: number) => {
    for (;;) {
      const d = c.deadline();
      if (d === null || d > until) break;
      t = Math.max(t, d);
      out.push(...c.tick(t));
    }
    t = until;
  };
  const wait = (ms: number) => run(t + ms);
  const d = {
    out,
    get t() {
      return t;
    },
    wait,
    down: (x: number, y: number, id = 1) => out.push(...c.down(id, x, y, t)),
    move: (x: number, y: number, id = 1) => out.push(...c.move(id, x, y, t)),
    up: (x: number, y: number, id = 1) => out.push(...c.up(id, x, y, t)),
    tap: (x: number, y: number, hold = 60) => {
      d.down(x, y);
      wait(hold);
      d.up(x, y);
    },
    /** A one-finger stroke from a to b over ms at hz events a second. */
    stroke: (a: [number, number], b: [number, number], ms: number, { hz = 120, id = 1, lift = true, rest = 0 } = {}) => {
      d.down(a[0], a[1], id);
      if (rest) wait(rest);
      const n = Math.max(1, Math.round((ms * hz) / 1000));
      for (let i = 1; i <= n; i++) {
        wait(ms / n);
        d.move(a[0] + ((b[0] - a[0]) * i) / n, a[1] + ((b[1] - a[1]) * i) / n, id);
      }
      if (lift) d.up(b[0], b[1], id);
    },
    take: () => out.splice(0),
  };
  return d;
}

const remote = (c: TouchControl, x: number, y: number) => c.toRemote({ x, y });
const kinds = (actions: TouchAction[]) => actions.map((a) => a.type).filter((k) => k !== "pointer" && k !== "view");
const sent = (actions: TouchAction[]) => actions.filter((a) => a.type === "move" || a.type === "button" || a.type === "click" || a.type === "wheel" || a.type === "scroll");
const notches = (actions: TouchAction[]) => actions.reduce((n, a) => n + (a.type === "wheel" ? a.dy : 0), 0);

describe("one finger acts on the computer", () => {
  test("a tap clicks where the finger is, with a ring, and the pointer goes there (resting)", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    const at = remote(c, 100, 300);
    expect(d.out).toContainEqual({ type: "click", x: at.x, y: at.y, button: "left" });
    expect(d.out).toContainEqual({ type: "ring", x: 100, y: 300 });
    expect(c.pointer).toMatchObject({ x: at.x, y: at.y, shown: true, aimed: false, pressed: false });
  });

  test("D2: tapping twice is a double click on one point (no zoom), the second tap snapped onto the first", () => {
    const c = desk();
    const d = driver(c);
    const view = { ...c.view };
    d.tap(100, 300);
    d.wait(120);
    d.tap(104, 302);
    const clicks = d.out.filter((a) => a.type === "click");
    expect(clicks).toHaveLength(2);
    expect(clicks[1]).toEqual(clicks[0]);
    expect(c.view).toEqual(view);
  });

  test("taps far apart or slow are two clicks where each landed", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(120);
    d.tap(140, 300);
    d.wait(600);
    d.tap(140, 300);
    const clicks = d.out.filter((a) => a.type === "click") as Extract<TouchAction, { type: "click" }>[];
    expect(clicks[1].x).toBeCloseTo(remote(c, 140, 300).x);
    expect(clicks[2].x).toBeCloseTo(remote(c, 140, 300).x);
  });

  test("a drag scrolls what is under the finger, content following it (wheel notches of the finger's travel)", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([200, 470], [200, 320], 600, { hz: 60 });
    d.wait(40);
    const wheels = d.out.filter((a) => a.type === "wheel") as Extract<TouchAction, { type: "wheel" }>[];
    const step = TOUCH.NOTCH_REMOTE * c.view.s;
    // 150 pt of travel is 4.1 notches of 36.6 pt: four now, the rest kept.
    expect(notches(d.out)).toBe(Math.trunc(150 / step));
    expect(wheels[0].x).toBeCloseTo(remote(c, 200, 470).x);
    expect(d.out.some((a) => a.type === "button" || a.type === "click")).toBe(false);
  });

  test("D5: a short deliberate drag (20 pt) still scrolls one notch at the lift", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([200, 470], [200, 450], 300, { hz: 60 });
    d.wait(40);
    expect(notches(d.out)).toBe(1);
  });

  test("D5: travel short of a notch carries into the next drag, not after 1.5 s", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([200, 470], [200, 455], 300, { hz: 60 });
    d.wait(100);
    d.stroke([200, 470], [200, 455], 300, { hz: 60 });
    expect(notches(d.out)).toBe(1);
    const e = driver(desk());
    e.stroke([200, 470], [200, 455], 300, { hz: 60 });
    e.wait(2000);
    e.stroke([200, 470], [200, 455], 300, { hz: 60 });
    expect(notches(e.out)).toBe(0);
  });

  test("D5: a flick keeps scrolling after the lift and slows to a stop, bounded", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([200, 470], [200, 390], 80, { hz: 120 });
    const atLift = notches(d.out);
    expect(c.flinging).toBe(true);
    d.wait(400);
    const mid = notches(d.out);
    d.wait(2000);
    const total = notches(d.out);
    expect(mid).toBeGreaterThan(atLift);
    expect(total).toBeGreaterThan(mid);
    expect(total - atLift).toBeLessThanOrEqual(TOUCH.FLING_MAX_NOTCHES);
    expect(c.flinging).toBe(false);
    // The gaps between notches grow: it slows.
    const times: number[] = [];
    const e = driver(desk());
    e.stroke([200, 470], [200, 390], 80, { hz: 120 });
    e.take();
    for (let i = 0; i < 80; i++) {
      e.wait(16);
      if (e.take().some((a) => a.type === "wheel")) times.push(e.t);
    }
    const gaps = times.slice(1).map((t, i) => t - times[i]);
    expect(gaps[gaps.length - 1]).toBeGreaterThan(gaps[0]);
  });

  test("a finger that stopped before lifting does not flick", () => {
    const d = driver(desk());
    d.stroke([200, 470], [200, 390], 80, { hz: 120, lift: false });
    d.wait(200);
    d.up(200, 390);
    const before = notches(d.out);
    d.wait(1000);
    expect(notches(d.out)).toBe(before);
  });

  test("a touch stops a flick and is not a click", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([200, 470], [200, 390], 80, { hz: 120 });
    d.wait(100);
    d.tap(200, 380);
    const n = notches(d.out);
    d.wait(1000);
    expect(notches(d.out)).toBe(n);
    expect(d.out.some((a) => a.type === "click")).toBe(false);
  });

  test("D3: press and hold, then lift, is a right click, in one gesture (haptic, pressed pointer first)", () => {
    const c = desk();
    const d = driver(c);
    d.down(100, 300);
    d.wait(TOUCH.HOLD_MS + 10);
    expect(d.out).toContainEqual({ type: "haptic" });
    expect(c.pointer.pressed).toBe(true);
    d.up(100, 300);
    const at = remote(c, 100, 300);
    expect(d.out).toContainEqual({ type: "click", x: at.x, y: at.y, button: "right" });
    expect(c.pointer.pressed).toBe(false);
  });

  test("press and hold, then drag, drags with the button held from where it was pressed", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([100, 300], [200, 300], 300, { rest: TOUCH.HOLD_MS + 20 });
    const s = sent(d.out);
    const a = remote(c, 100, 300);
    const b = remote(c, 200, 300);
    expect(s.find((x) => x.type === "button")).toEqual({ type: "button", x: a.x, y: a.y, down: true });
    expect(s[s.length - 1]).toEqual({ type: "button", x: b.x, y: b.y, down: false });
    expect(s.some((x) => x.type === "wheel" || x.type === "click")).toBe(false);
  });

  test("the hold fires only if the finger stayed within the slop for the whole hold: moving first scrolls", () => {
    const c = desk();
    const d = driver(c);
    d.down(200, 470);
    d.wait(100);
    d.move(200, 450);
    d.wait(TOUCH.HOLD_MS + 200);
    d.move(200, 380);
    d.up(200, 380);
    expect(d.out.some((a) => a.type === "haptic" || a.type === "button")).toBe(false);
    expect(notches(d.out)).toBeGreaterThan(0);
  });

  test("finger down, read, then scroll stays a scroll: a hold rested on lapses, visibly, and a move after it scrolls", () => {
    const c = desk();
    const d = driver(c);
    d.down(200, 470);
    d.wait(TOUCH.HOLD_MS + 20);
    expect(c.pointer.pressed).toBe(true);
    d.wait(TOUCH.HOLD_LAPSE_MS + 20);
    expect(c.pointer.pressed).toBe(false);
    d.wait(1000);
    for (let i = 1; i <= 20; i++) {
      d.wait(16);
      d.move(200, 470 - i * 5);
    }
    d.up(200, 370);
    expect(d.out.some((a) => a.type === "button" || a.type === "click")).toBe(false);
    expect(notches(d.out)).toBeGreaterThan(0);
    // A hold rested on and lifted is nothing at all.
    const e = driver(desk());
    e.down(100, 300);
    e.wait(2000);
    e.up(100, 300);
    expect(sent(e.out).filter((a) => a.type !== "move")).toEqual([]);
  });

  test("a touch beside the picture does nothing on the computer", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 100);
    d.stroke([100, 120], [100, 60], 200);
    expect(sent(d.out)).toEqual([]);
  });
});

describe("two fingers move the view", () => {
  test("a pinch zooms the view around the fingers; nothing reaches the computer", () => {
    const c = desk();
    const d = driver(c);
    const fit = c.view.s;
    d.down(150, 360, 1);
    d.down(240, 360, 2);
    for (let i = 1; i <= 10; i++) {
      d.wait(16);
      d.move(150 - i * 6, 360, 1);
      d.move(240 + i * 6, 360, 2);
    }
    d.up(90, 360, 1);
    d.up(300, 360, 2);
    expect(c.view.s).toBeGreaterThan(fit * 1.5);
    expect(sent(d.out)).toEqual([]);
  });

  test("a two-finger drag moves the zoomed view", () => {
    const c = desk();
    c.zoomAt(c.view.fit * 3, { x: 195, y: 360 });
    const d = driver(c);
    const tx = c.view.tx;
    d.down(150, 360, 1);
    d.down(240, 360, 2);
    for (let i = 1; i <= 10; i++) {
      d.wait(16);
      d.move(150 + i * 8, 360, 1);
      d.move(240 + i * 8, 360, 2);
    }
    expect(c.view.tx).toBeCloseTo(tx + 80, 0);
    expect(sent(d.out)).toEqual([]);
  });

  test("a two-finger tap is a right click between the fingers", () => {
    const c = desk();
    const d = driver(c);
    d.down(150, 360, 1);
    d.down(190, 360, 2);
    d.wait(80);
    d.up(150, 360, 1);
    d.up(190, 360, 2);
    const at = remote(c, 170, 360);
    expect(sent(d.out)).toEqual([{ type: "click", x: at.x, y: at.y, button: "right" }]);
  });

  test("a second finger lets go of a held button, and lifting back to one finger does nothing", () => {
    const c = desk();
    const d = driver(c);
    d.stroke([100, 300], [150, 300], 200, { rest: TOUCH.HOLD_MS + 20, lift: false });
    d.down(250, 300, 2);
    expect(d.out.filter((a) => a.type === "button").map((a) => (a as { down: boolean }).down)).toEqual([true, false]);
    d.take();
    d.wait(400);
    d.up(250, 300, 2);
    d.move(170, 320);
    d.up(170, 320);
    expect(sent(d.out)).toEqual([]);
  });

  test("D4: zoomed in, one finger still scrolls the computer (never the view), a notch at most 40 pt of travel", () => {
    const c = desk();
    c.zoomAt(2, { x: 195, y: 360 });
    const d = driver(c);
    const view = { ...c.view };
    d.stroke([200, 470], [200, 320], 600, { hz: 60 });
    expect(c.view).toEqual(view);
    // Three whole notches of 40 pt, and the last 30 pt (past half a notch) at the lift.
    expect(notches(d.out)).toBe(Math.round(150 / TOUCH.NOTCH_MAX));
  });
});

describe("the pointer's handle (whole desktops)", () => {
  const knob = (c: TouchControl) => c.handlePlace()!;

  test("after a tap the handle rests below-right of the pointer; a tap on it is a tap at the finger", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    const h = knob(c);
    expect(h.x).toBeCloseTo(100 + TOUCH.KNOB_DX);
    expect(h.y).toBeCloseTo(300 + TOUCH.KNOB_DY);
    d.take();
    d.wait(600);
    d.tap(h.x, h.y);
    const at = remote(c, h.x, h.y);
    expect(sent(d.out)).toContainEqual({ type: "click", x: at.x, y: at.y, button: "left" });
  });

  test("D6/T3: dragging the handle moves the pointer at once (a 6 pt nudge too), with no button: hover", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    const before = { ...c.pointer };
    const h = knob(c);
    d.take();
    d.wait(600);
    d.stroke([h.x, h.y], [h.x + 6, h.y], 200, { hz: 60 });
    expect(c.pointer.x).toBeGreaterThan(before.x + 4);
    expect(d.out.every((a) => a.type !== "button" && a.type !== "click" && a.type !== "wheel")).toBe(true);
    expect(d.out.filter((a) => a.type === "move").length).toBeGreaterThan(3);
    expect(c.pointer.aimed).toBe(true);
  });

  test("D1: slow strokes are finer than the finger, fast ones go further", () => {
    const slow = desk();
    const ds = driver(slow);
    ds.tap(100, 300);
    ds.wait(600);
    let h = knob(slow);
    const x0 = slow.pointer.x;
    ds.stroke([h.x, h.y], [h.x + 60, h.y], 3000);
    const slowTravel = (slow.pointer.x - x0) * slow.view.s;
    const fast = desk();
    const df = driver(fast);
    df.tap(100, 300);
    df.wait(600);
    h = knob(fast);
    const x1 = fast.pointer.x;
    df.stroke([h.x, h.y], [h.x + 60, h.y], 60);
    const fastTravel = (fast.pointer.x - x1) * fast.view.s;
    expect(slowTravel).toBeLessThan(60 * 0.6);
    expect(fastTravel).toBeGreaterThan(60);
  });

  test("T4: the same stroke moves the pointer as far at 60 Hz as at 120 Hz", () => {
    const travel = (hz: number) => {
      const c = desk();
      const d = driver(c);
      d.tap(100, 300);
      d.wait(600);
      const h = knob(c);
      const x0 = c.pointer.x;
      d.stroke([h.x, h.y], [h.x + 100, h.y], 400, { hz });
      return c.pointer.x - x0;
    };
    expect(Math.abs(travel(60) / travel(120) - 1)).toBeLessThan(0.05);
  });

  test("once aimed, the handle is solid and a tap on it clicks at the tip; again: a double click there", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(600);
    let h = knob(c);
    d.stroke([h.x, h.y], [h.x + 20, h.y + 10], 300);
    const tip = { x: c.pointer.x, y: c.pointer.y };
    expect(c.pointer.aimed).toBe(true);
    d.take();
    d.wait(300);
    h = knob(c);
    d.tap(h.x + 3, h.y - 2);
    d.wait(150);
    d.tap(h.x, h.y);
    const clicks = d.out.filter((a) => a.type === "click");
    expect(clicks).toEqual([
      { type: "click", ...tip, button: "left" },
      { type: "click", ...tip, button: "left" },
    ]);
    expect(c.pointer).toMatchObject({ ...tip, aimed: true });
  });

  test("T1: a finger resting on the handle before it moves moves the pointer: it never presses", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(600);
    const h = knob(c);
    d.take();
    d.stroke([h.x, h.y], [h.x + 60, h.y], 300, { rest: 600 });
    expect(d.out.some((a) => a.type === "button" || a.type === "click")).toBe(false);
    expect(c.pointer.aimed).toBe(true);
  });

  test("holding the handle, then dragging, drags from the tip; holding and lifting is a right click at the tip", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(600);
    let h = knob(c);
    const tip = { x: c.pointer.x, y: c.pointer.y };
    d.take();
    d.stroke([h.x, h.y], [h.x + 40, h.y], 300, { rest: TOUCH.HANDLE_HOLD_MS + 20 });
    const s = sent(d.out);
    expect(s.find((a) => a.type === "button")).toEqual({ type: "button", ...tip, down: true });
    expect(s[s.length - 1]).toMatchObject({ type: "button", down: false });
    expect(c.pointer.pressed).toBe(false);
    d.take();
    d.wait(600);
    h = knob(c);
    const tip2 = { x: c.pointer.x, y: c.pointer.y };
    d.down(h.x, h.y);
    d.wait(TOUCH.HANDLE_HOLD_MS + 20);
    expect(c.pointer.pressed).toBe(true);
    d.up(h.x, h.y);
    expect(sent(d.out)).toEqual([{ type: "click", ...tip2, button: "right" }]);
  });

  test("a handle held and then rested on lapses: a move after it moves the pointer, never presses", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(600);
    const h = knob(c);
    d.take();
    d.stroke([h.x, h.y], [h.x + 60, h.y], 300, { rest: TOUCH.HANDLE_HOLD_MS + TOUCH.HOLD_LAPSE_MS + 100 });
    expect(d.out.some((a) => a.type === "button" || a.type === "click")).toBe(false);
    expect(c.pointer.aimed).toBe(true);
  });

  test("T2: moving right after a tap scrolls or moves the pointer: never a drag", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(150);
    d.stroke([100, 330], [100, 260], 200);
    d.wait(1500);
    expect(d.out.some((a) => a.type === "button")).toBe(false);
    expect(d.out.filter((a) => a.type === "click")).toHaveLength(1);
  });

  test("a tap just below the last one, off the knob, clicks there; a drag starting there scrolls", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    d.wait(600);
    d.take();
    d.tap(100, 318);
    const at = remote(c, 100, 318);
    expect(sent(d.out)).toContainEqual({ type: "click", x: at.x, y: at.y, button: "left" });
    d.take();
    d.wait(600);
    d.stroke([100, 330], [100, 250], 400, { hz: 60 });
    expect(notches(d.out)).toBeGreaterThan(0);
  });

  test("the handle flips away from the view's right and bottom edges (and the system gesture area)", () => {
    const c = desk();
    const d = driver(c);
    d.tap(380, 300);
    expect(knob(c).x).toBeLessThan(380);
    // Zoomed in, the picture reaches the view's bottom, where the phone's own gestures are.
    c.zoomAt(2, { x: 195, y: 360 });
    d.wait(600);
    d.tap(100, 690);
    expect(knob(c).y).toBeLessThan(690);
    d.wait(600);
    d.tap(100, 650);
    expect(knob(c).y).toBeLessThan(650);
    expect(knob(c).y + TOUCH.KNOB_R).toBeLessThanOrEqual(720 - TOUCH.EDGE_BOTTOM);
  });

  test("a pointer outside the zoomed view is not drawn and has no handle; the next tap places it again", () => {
    const c = desk();
    const d = driver(c);
    d.tap(30, 300);
    c.zoomAt(2, { x: 380, y: 360 });
    expect(c.visible()).toBe(false);
    expect(c.handlePlace()).toBeNull();
    d.wait(600);
    d.tap(200, 360);
    expect(c.handlePlace()).not.toBeNull();
  });

  test("a page laid out for the phone has no pointer drawn; a hold shows it pressed, then it goes", () => {
    const c = desk({ handle: false });
    const d = driver(c);
    d.tap(100, 300);
    expect(c.pointer.shown).toBe(false);
    expect(c.handlePlace()).toBeNull();
    d.down(100, 300);
    d.wait(TOUCH.HOLD_MS + 10);
    expect(c.pointer).toMatchObject({ shown: true, pressed: true });
    d.up(100, 300);
    expect(c.pointer.shown).toBe(false);
  });
});

describe("fine scroll (computers that take pixel scrolls)", () => {
  test("content follows the finger exactly, at any zoom, and a flick decays smoothly", () => {
    const c = desk({ fineScroll: true });
    const d = driver(c);
    d.stroke([200, 470], [200, 320], 600, { hz: 60 });
    const total = d.out.reduce((n, a) => n + (a.type === "scroll" ? a.dy : 0), 0);
    expect(Math.abs(total - 150 / c.view.s)).toBeLessThanOrEqual(1);
    expect(d.out.some((a) => a.type === "wheel")).toBe(false);
    c.zoomAt(2, { x: 195, y: 360 });
    const e = driver(c);
    e.stroke([200, 470], [200, 420], 600, { hz: 60 });
    const zoomed = e.out.reduce((n, a) => n + (a.type === "scroll" ? a.dy : 0), 0);
    expect(Math.abs(zoomed - 50 / c.view.s)).toBeLessThanOrEqual(1);
    const f = driver(desk({ fineScroll: true }));
    f.stroke([200, 470], [200, 390], 80, { hz: 120 });
    f.take();
    const steps: number[] = [];
    for (let i = 0; i < 60; i++) {
      f.wait(16);
      steps.push(f.take().reduce((n, a) => n + (a.type === "scroll" ? a.dy : 0), 0));
    }
    expect(steps[0]).toBeGreaterThan(0);
    expect(steps[10]).toBeLessThan(steps[0]);
    expect(steps.filter((n) => n > 0).length).toBeGreaterThan(20);
  });
});

describe("the zoom button and fit", () => {
  test("one hand zooms in around the pointer, and back to fit", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    const p = c.toLocal(c.pointer);
    c.zoomButton();
    expect(c.view.s).toBeCloseTo(c.view.fit * TOUCH.ZOOM_STEP);
    const q = c.toLocal(c.pointer);
    expect(Math.abs(q.x - p.x)).toBeLessThan(1);
    c.zoomButton();
    expect(c.fitted()).toBe(true);
  });

  test("a new picture size fits it and forgets the pointer and the last tap", () => {
    const c = desk();
    const d = driver(c);
    d.tap(100, 300);
    c.picture(1082, 1995);
    expect(c.pointer.shown).toBe(false);
    expect(c.view.s).toBeCloseTo(Math.min(390 / 1082, 720 / 1995));
    expect(kinds(d.out)).toContain("click");
  });
});
