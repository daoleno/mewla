// Screen's touch control: one model for using a computer's screen with fingers, as a state machine
// with no browser in it (touches and time in, actions out). The viewer page (client/ui/more/screen/
// runtime/viewer.js) feeds it pointer events and timer ticks, draws the view and the pointer it
// describes, and sends its actions to the computer.
//
// One finger acts on the computer: a tap clicks where the finger is (two taps are a double click),
// a drag scrolls what is under the finger (a flick keeps going), a press held still and lifted is a
// right click, held still and then dragged drags with the button held. Two fingers move the view
// (pinch, pan) and never reach the computer, except a two-finger tap: a right click. On a whole
// desktop the computer's pointer is drawn with a handle: dragging the handle moves the pointer
// precisely (hover on the way), holding it and dragging drags from the pointer's tip, and once the
// pointer was placed with the handle a tap on the handle clicks at the tip.

/** Local view units are CSS pixels of the viewer page; remote units are the picture's pixels. */
export interface TouchView {
  /** CSS pixels per picture pixel. */
  s: number;
  tx: number;
  ty: number;
  /** The scale at which the whole picture fits. */
  fit: number;
}

export type TouchAction =
  /** Move the computer's pointer (the buttons stay as they are). */
  | { type: "move"; x: number; y: number }
  /** Press or let go of the left button there. */
  | { type: "button"; x: number; y: number; down: boolean }
  | { type: "click"; x: number; y: number; button: "left" | "right" }
  /** Scroll by whole wheel notches there (+y: down, +x: right). */
  | { type: "wheel"; x: number; y: number; dx: number; dy: number }
  /** Scroll by picture pixels there (+y: the page moves up, as a wheel turned down), where the computer takes them. */
  | { type: "scroll"; x: number; y: number; dx: number; dy: number }
  /** The view (zoom, position) changed. */
  | { type: "view" }
  /** The drawn pointer changed. */
  | { type: "pointer" }
  /** A ring where a tap landed (local point). */
  | { type: "ring"; x: number; y: number }
  | { type: "haptic" };

export interface PointerState {
  /** Where the computer's pointer is, in picture pixels. */
  x: number;
  y: number;
  /** Drawn at all. */
  shown: boolean;
  /** Placed with the handle: the handle is solid and a tap on it clicks at the tip. */
  aimed: boolean;
  /** A button is held (or a press is being held): drawn pressed. */
  pressed: boolean;
}

/** The handle's centre relative to the pointer's tip, and which way it was flipped (local px). */
export interface HandlePlace {
  x: number;
  y: number;
}

export const TOUCH = {
  /** A finger moving less than this is still a tap or a hold. */
  SLOP: 10,
  /** On the handle any visible move counts: only a tap's own jitter is less. */
  HANDLE_SLOP: 4,
  HOLD_MS: 450,
  /** Holding the handle takes longer: a careful start (the finger resting before it moves the pointer) must not press. */
  HANDLE_HOLD_MS: 700,
  /** A second tap this soon and this near lands on the first one's point, so the computer counts a double click. */
  DOUBLE_MS: 400,
  DOUBLE_SNAP: 12,
  /** Two fingers lifted this soon, unmoved, are a two-finger tap. */
  TWO_TAP_MS: 300,
  TWO_SLOP: 12,
  TWO_PINCH: 20,
  /** One wheel notch is this many picture pixels; its finger travel is kept between these (local px). */
  NOTCH_REMOTE: 120,
  NOTCH_MIN: 6,
  NOTCH_MAX: 40,
  /** Travel short of a notch is kept this long for the next drag. */
  CARRY_MS: 1500,
  /** A lift faster than this (local px/ms) flicks. */
  FLING_MIN: 0.35,
  FLING_STOP: 0.03,
  /** Velocity falls by e every this many ms. */
  FLING_TAU: 325,
  FLING_MAX_MS: 1200,
  FLING_MAX_NOTCHES: 24,
  FRAME_MS: 16,
  /** The handle: its radius (drawn and touched), its offset from the tip, and the room kept from the view's edges. */
  KNOB_R: 16,
  KNOB_DX: 22,
  KNOB_DY: 38,
  EDGE: 12,
  /** Room kept above the bottom edge: the phone's system gesture area. */
  EDGE_BOTTOM: 28,
  /** Handle gain: picture travel per finger travel, slow to fast (velocity in local px/ms). */
  GAIN_MIN: 0.35,
  GAIN_MAX: 2,
  GAIN_PER_SPEED: 2.2,
  /** Zoom bounds: CSS px per picture px at most, and the zoom button's step past fit. */
  MAX_SCALE: 2,
  ZOOM_STEP: 2.5,
} as const;

type Point = { x: number; y: number };
type Sample = Point & { t: number };

type One =
  | { kind: "pending"; start: Sample; last: Sample; remote: Point; held: boolean; beside: boolean; caught: boolean }
  | { kind: "scroll"; last: Sample; remote: Point; samples: Sample[] }
  | { kind: "drag"; last: Sample }
  | { kind: "handle"; start: Sample; last: Sample; origin: Point; aimed: boolean; held: boolean; moved: boolean; speed: number }
  | { kind: "handle-drag"; last: Sample; speed: number };

type Gesture =
  | { kind: "one"; id: number; g: One; at: number }
  | { kind: "two"; ids: [number, number]; start: { dist: number; c: Point }; last: { dist: number; c: Point }; scale: number; at: number; decided: boolean }
  | { kind: "done" };

export interface TouchOptions {
  /** The computer takes picture-pixel scrolls (`scroll` actions) instead of wheel notches. */
  fineScroll?: boolean;
  /** The pointer and its handle are drawn: whole desktops, not a page laid out for the phone. */
  handle?: boolean;
}

export class TouchControl {
  readonly view: TouchView = { s: 1, tx: 0, ty: 0, fit: 1 };
  readonly pointer: PointerState = { x: 0, y: 0, shown: false, aimed: false, pressed: false };
  /** Where a picture shorter than the view sits. */
  align: "top" | "center" = "center";
  fineScroll: boolean;
  private handleOn: boolean;
  private fbW = 0;
  private fbH = 0;
  private vw = 0;
  private vh = 0;
  private touches = new Map<number, Point>();
  private gesture: Gesture | null = null;
  private holdAt = 0;
  private lastTap: { at: number; x: number; y: number; remote: Point } | null = null;
  private carry = { x: 0, y: 0, at: 0, fine: { x: 0, y: 0 } };
  private fling: { vx: number; vy: number; at: number; start: number; notches: number; remote: Point } | null = null;

  constructor(options: TouchOptions = {}) {
    this.fineScroll = !!options.fineScroll;
    this.handleOn = options.handle ?? true;
  }

  // ---- the frame of reference ------------------------------------------------

  /** The view's size (CSS px). Keeps a fitted view fitted. */
  viewport(width: number, height: number): TouchAction[] {
    const wasFit = this.fitted();
    this.vw = width;
    this.vh = height;
    if (!this.fbW) return [];
    this.view.fit = Math.min(width / this.fbW, height / this.fbH);
    if (wasFit) return this.fitView();
    this.clamp();
    return [{ type: "view" }];
  }

  /** The view's size changed without refitting (the keyboard came up or went): the caller moves the view. */
  size(width: number, height: number): void {
    this.vw = width;
    this.vh = height;
    if (this.fbW) this.view.fit = Math.min(width / this.fbW, height / this.fbH);
  }

  /** A new picture size: the view fits it, and nothing aimed at the old one stays. */
  picture(width: number, height: number): TouchAction[] {
    if (width === this.fbW && height === this.fbH) return [];
    this.fbW = width;
    this.fbH = height;
    this.lastTap = null;
    this.stopFling();
    Object.assign(this.pointer, { x: width / 2, y: height / 2, shown: false, aimed: false, pressed: false });
    return [...this.fitView(), { type: "pointer" }];
  }

  get width(): number {
    return this.fbW;
  }

  get height(): number {
    return this.fbH;
  }

  /** Whether the pointer and its handle are drawn (whole desktops). */
  setHandle(on: boolean): TouchAction[] {
    if (on === this.handleOn) return [];
    this.handleOn = on;
    if (!on) Object.assign(this.pointer, { shown: false, aimed: false });
    return [{ type: "pointer" }];
  }

  get handle(): boolean {
    return this.handleOn;
  }

  fitView(): TouchAction[] {
    if (!this.fbW || !this.vw) return [];
    this.view.fit = Math.min(this.vw / this.fbW, this.vh / this.fbH);
    this.view.s = this.view.fit;
    this.clamp();
    return [{ type: "view" }];
  }

  fitted(): boolean {
    return this.view.s <= this.view.fit * 1.02;
  }

  /** The zoom button: in around the pointer (or the middle), and back to fit. */
  zoomButton(): TouchAction[] {
    if (!this.fitted()) return this.fitView();
    const at = this.pointer.shown && this.visible() ? this.toLocal(this.pointer) : { x: this.vw / 2, y: this.vh / 2 };
    this.zoomAt(this.view.fit * TOUCH.ZOOM_STEP, at);
    return [{ type: "view" }];
  }

  clamp(): void {
    const v = this.view;
    const w = this.fbW * v.s;
    const h = this.fbH * v.s;
    v.tx = w <= this.vw ? (this.vw - w) / 2 : Math.min(0, Math.max(this.vw - w, v.tx));
    v.ty = h <= this.vh ? (this.align === "top" ? 0 : (this.vh - h) / 2) : Math.min(0, Math.max(this.vh - h, v.ty));
  }

  zoomAt(scale: number, at: Point): void {
    const v = this.view;
    const next = Math.max(v.fit, Math.min(Math.max(v.fit, TOUCH.MAX_SCALE), scale));
    const fx = (at.x - v.tx) / v.s;
    const fy = (at.y - v.ty) / v.s;
    v.s = next;
    v.tx = at.x - fx * next;
    v.ty = at.y - fy * next;
    this.clamp();
  }

  toRemote(p: Point): Point {
    return { x: (p.x - this.view.tx) / this.view.s, y: (p.y - this.view.ty) / this.view.s };
  }

  toLocal(p: Point): Point {
    return { x: this.view.tx + p.x * this.view.s, y: this.view.ty + p.y * this.view.s };
  }

  private beside(remote: Point): boolean {
    return remote.x < 0 || remote.y < 0 || remote.x > this.fbW || remote.y > this.fbH;
  }

  private clampRemote(p: Point): Point {
    return { x: Math.max(0, Math.min(this.fbW - 1, p.x)), y: Math.max(0, Math.min(this.fbH - 1, p.y)) };
  }

  /** The pointer's tip is inside the view. Outside it (zoomed in, the view moved away) nothing is drawn: the next tap places it again. */
  visible(): boolean {
    const p = this.toLocal(this.pointer);
    return p.x >= 0 && p.y >= 0 && p.x <= this.vw && p.y <= this.vh;
  }

  /** The handle's centre (local px), on the side of the tip with room: below and right, else left, else above. */
  handlePlace(): HandlePlace | null {
    if (!this.handleOn || !this.pointer.shown || !this.visible()) return null;
    const tip = this.toLocal(this.pointer);
    const r = TOUCH.KNOB_R;
    const right = tip.x + TOUCH.KNOB_DX + r + TOUCH.EDGE <= this.vw;
    const below = tip.y + TOUCH.KNOB_DY + r + TOUCH.EDGE_BOTTOM <= this.vh;
    return { x: tip.x + (right ? TOUCH.KNOB_DX : -TOUCH.KNOB_DX), y: tip.y + (below ? TOUCH.KNOB_DY : -TOUCH.KNOB_DY) };
  }

  private onHandle(p: Point): boolean {
    const h = this.handlePlace();
    return !!h && Math.hypot(p.x - h.x, p.y - h.y) <= TOUCH.KNOB_R;
  }

  // ---- input ---------------------------------------------------------------------

  /** The next time `tick` has something to do, or null. */
  deadline(): number | null {
    if (this.fling) return this.fling.at + TOUCH.FRAME_MS;
    if (this.holdAt) return this.holdAt;
    return null;
  }

  tick(t: number): TouchAction[] {
    const out: TouchAction[] = [];
    if (this.holdAt && t >= this.holdAt) {
      this.holdAt = 0;
      const g = this.gesture;
      if (g?.kind === "one" && ((g.g.kind === "pending" && !g.g.caught) || g.g.kind === "handle") && !g.g.held) {
        g.g.held = true;
        out.push({ type: "haptic" });
        if (g.g.kind === "pending" && !g.g.beside) {
          // Held still: the pointer goes there, pressed (a hover on the way to the right click or drag).
          Object.assign(this.pointer, this.clampRemote(g.g.remote), { shown: true, pressed: true, aimed: false });
          out.push({ type: "move", ...this.clampRemote(g.g.remote) }, { type: "pointer" });
        } else if (g.g.kind === "handle") {
          this.pointer.pressed = true;
          out.push({ type: "pointer" });
        }
      }
    }
    if (this.fling) this.flingStep(t, out);
    return out;
  }

  down(id: number, x: number, y: number, t: number): TouchAction[] {
    const out: TouchAction[] = [];
    if (!this.fbW) return out;
    const caught = this.fling !== null;
    this.stopFling();
    this.touches.set(id, { x, y });
    const count = this.touches.size;
    if (count === 1) {
      const p = { x, y, t };
      if (this.onHandle(p)) {
        this.gesture = { kind: "one", id, at: t, g: { kind: "handle", start: p, last: p, origin: { x: this.pointer.x, y: this.pointer.y }, aimed: this.pointer.aimed, held: false, moved: false, speed: 0 } };
      } else {
        const remote = this.toRemote(p);
        this.gesture = { kind: "one", id, at: t, g: { kind: "pending", start: p, last: p, remote, held: false, beside: this.beside(remote), caught } };
      }
      this.holdAt = t + (this.gesture.g.kind === "handle" ? TOUCH.HANDLE_HOLD_MS : TOUCH.HOLD_MS);
      return out;
    }
    if (count === 2 && this.gesture?.kind === "one") {
      // A second finger: whatever one finger was doing ends (a held button is let go), and the view is the fingers'.
      this.endOne(out, t, true);
      const [a, b] = [...this.touches.values()];
      const s = { dist: Math.hypot(a.x - b.x, a.y - b.y), c: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } };
      this.gesture = { kind: "two", ids: [...this.touches.keys()] as [number, number], start: s, last: s, scale: this.view.s, at: t, decided: false };
      return out;
    }
    // A third finger, or a second one after a two-finger gesture ended: nothing until every finger is up.
    if (this.gesture?.kind === "two") this.gesture = { kind: "done" };
    return out;
  }

  move(id: number, x: number, y: number, t: number): TouchAction[] {
    const out: TouchAction[] = [];
    if (!this.touches.has(id)) return out;
    this.touches.set(id, { x, y });
    const gesture = this.gesture;
    if (!gesture) return out;
    if (gesture.kind === "two") {
      this.moveTwo(gesture, out);
      return out;
    }
    if (gesture.kind !== "one" || gesture.id !== id) return out;
    const g = gesture.g;
    const p = { x, y, t };
    switch (g.kind) {
      case "pending": {
        const far = Math.hypot(x - g.start.x, y - g.start.y) > TOUCH.SLOP;
        if (!far) {
          g.last = p;
          return out;
        }
        this.holdAt = 0;
        if (g.beside) {
          // Begun beside the picture: not the computer's.
          gesture.g = { kind: "scroll", last: p, remote: { x: -1, y: -1 }, samples: [] };
          return out;
        }
        if (g.held) {
          // Held still, then moved: drag with the button held from where it was pressed.
          const at = this.clampRemote(g.remote);
          out.push({ type: "button", ...at, down: true });
          gesture.g = { kind: "drag", last: p };
          this.dragTo(p, out);
          return out;
        }
        // Content follows the finger from where it went down: no travel is lost to the slop.
        const s: One = { kind: "scroll", last: g.start, remote: this.clampRemote(g.remote), samples: [g.start] };
        gesture.g = s;
        this.scrollTo(s, p, out);
        return out;
      }
      case "scroll":
        if (g.remote.x < 0) return out; // began beside the picture
        this.scrollTo(g, p, out);
        return out;
      case "drag":
        g.last = p;
        this.dragTo(p, out);
        return out;
      case "handle": {
        g.speed = speedOf(g.last, p, g.speed);
        this.nudge(p.x - g.last.x, p.y - g.last.y, g.speed);
        g.last = p;
        if (Math.hypot(x - g.start.x, y - g.start.y) > TOUCH.HANDLE_SLOP) {
          g.moved = true;
          this.holdAt = 0;
        }
        if (g.held && g.moved) {
          // Held, then moved: drag from the tip where the hold began.
          out.push({ type: "button", ...this.clampRemote(g.origin), down: true });
          gesture.g = { kind: "handle-drag", last: p, speed: g.speed };
          this.pointer.pressed = true;
        } else if (g.moved) this.pointer.aimed = true;
        out.push({ type: "move", x: this.pointer.x, y: this.pointer.y }, { type: "pointer" });
        return out;
      }
      case "handle-drag":
        g.speed = speedOf(g.last, p, g.speed);
        this.nudge(p.x - g.last.x, p.y - g.last.y, g.speed);
        g.last = p;
        out.push({ type: "move", x: this.pointer.x, y: this.pointer.y }, { type: "pointer" });
        return out;
    }
  }

  up(id: number, x: number, y: number, t: number): TouchAction[] {
    return this.lift(id, x, y, t, false);
  }

  cancel(id: number, t: number): TouchAction[] {
    const last = this.touches.get(id) ?? { x: 0, y: 0 };
    return this.lift(id, last.x, last.y, t, true);
  }

  private lift(id: number, x: number, y: number, t: number, cancelled: boolean): TouchAction[] {
    const out: TouchAction[] = [];
    if (!this.touches.has(id)) return out;
    const count = this.touches.size;
    this.touches.set(id, { x, y });
    const gesture = this.gesture;
    if (gesture?.kind === "two" && count === 2) {
      if (!gesture.decided && !cancelled && t - gesture.at < TOUCH.TWO_TAP_MS) {
        const at = this.toRemote(gesture.start.c);
        if (!this.beside(at)) {
          const r = this.clampRemote(at);
          out.push({ type: "click", ...r, button: "right" });
          this.place(r, false, out);
        }
      }
      // Lifting one of two fingers never turns into a tap or a drag.
      this.gesture = { kind: "done" };
    } else if (gesture?.kind === "one" && gesture.id === id) {
      if (gesture.g.kind === "drag" || gesture.g.kind === "pending") gesture.g.last = { x, y, t };
      this.endOne(out, t, cancelled);
      this.gesture = null;
    }
    this.touches.delete(id);
    if (this.touches.size === 0 && this.gesture?.kind === "done") this.gesture = null;
    return out;
  }

  // ---- one finger ----------------------------------------------------------------

  private endOne(out: TouchAction[], t: number, cancelled: boolean): void {
    const gesture = this.gesture;
    if (gesture?.kind !== "one") return;
    this.holdAt = 0;
    const g = gesture.g;
    switch (g.kind) {
      case "pending": {
        if (cancelled || g.beside || g.caught) {
          if (g.held) this.unpress(out);
          return;
        }
        const r = this.clampRemote(g.remote);
        if (g.held) {
          out.push({ type: "click", ...r, button: "right" });
          this.place(r, false, out);
          return;
        }
        this.tap(g.start, r, t, out);
        return;
      }
      case "scroll":
        if (!cancelled && g.remote.x >= 0) this.endScroll(g, t, out);
        return;
      case "drag": {
        const at = this.clampRemote(this.toRemote(g.last));
        out.push({ type: "button", ...at, down: false });
        this.place(at, false, out);
        return;
      }
      case "handle": {
        if (cancelled) {
          if (g.held) this.unpress(out);
          return;
        }
        if (g.held && !g.moved) {
          out.push({ type: "click", ...this.clampRemote(g.origin), button: "right" });
          this.place(g.origin, g.aimed, out);
          return;
        }
        if (g.moved) {
          this.pointer.aimed = true;
          out.push({ type: "pointer" });
          return;
        }
        // A tap on the handle: at the tip once the pointer was aimed with the handle; else a tap where the finger is.
        Object.assign(this.pointer, g.origin);
        if (g.aimed) {
          const r = this.clampRemote(g.origin);
          out.push({ type: "move", ...r }, { type: "click", ...r, button: "left" }, { type: "pointer" });
          this.lastTap = { at: t, x: g.start.x, y: g.start.y, remote: r };
          return;
        }
        const remote = this.toRemote(g.start);
        if (this.beside(remote)) return;
        this.tap(g.start, this.clampRemote(remote), t, out);
        return;
      }
      case "handle-drag":
        out.push({ type: "button", x: this.pointer.x, y: this.pointer.y, down: false });
        this.pointer.pressed = false;
        this.pointer.aimed = true;
        out.push({ type: "pointer" });
        return;
    }
  }

  private tap(local: Point, remote: Point, t: number, out: TouchAction[]): void {
    const last = this.lastTap;
    // The second of two quick taps lands on the first's point: the computer counts a double click there.
    const at = last && t - last.at < TOUCH.DOUBLE_MS && Math.hypot(last.x - local.x, last.y - local.y) <= TOUCH.DOUBLE_SNAP ? last.remote : remote;
    out.push({ type: "click", ...at, button: "left" }, { type: "ring", x: local.x, y: local.y });
    this.lastTap = { at: t, x: local.x, y: local.y, remote: at };
    this.place(at, false, out);
  }

  /** The pointer is where the computer's is now: drawn resting, or aimed (the handle placed it). */
  private place(at: Point, aimed: boolean, out: TouchAction[]): void {
    Object.assign(this.pointer, { x: at.x, y: at.y, shown: this.handleOn, aimed: this.handleOn && aimed, pressed: false });
    out.push({ type: "pointer" });
  }

  private unpress(out: TouchAction[]): void {
    this.pointer.pressed = false;
    if (!this.handleOn) this.pointer.shown = false;
    out.push({ type: "pointer" });
  }

  private dragTo(p: Point, out: TouchAction[]): void {
    const r = this.clampRemote(this.toRemote(p));
    Object.assign(this.pointer, r, { shown: true, pressed: true, aimed: false });
    out.push({ type: "move", ...r }, { type: "pointer" });
  }

  /** The handle moves the pointer by finger travel times a gain that grows with speed: slow is finer than the finger. */
  private nudge(dx: number, dy: number, speed: number): void {
    const gain = Math.max(TOUCH.GAIN_MIN, Math.min(TOUCH.GAIN_MAX, TOUCH.GAIN_MIN + TOUCH.GAIN_PER_SPEED * speed));
    const next = this.clampRemote({ x: this.pointer.x + (dx * gain) / this.view.s, y: this.pointer.y + (dy * gain) / this.view.s });
    this.pointer.x = next.x;
    this.pointer.y = next.y;
  }

  // ---- scroll ----------------------------------------------------------------------

  private notchLocal(): number {
    return Math.max(TOUCH.NOTCH_MIN, Math.min(TOUCH.NOTCH_MAX, TOUCH.NOTCH_REMOTE * this.view.s));
  }

  private scrollTo(g: Extract<One, { kind: "scroll" }>, p: Sample, out: TouchAction[]): void {
    const dx = p.x - g.last.x;
    const dy = p.y - g.last.y;
    g.last = p;
    g.samples.push(p);
    while (g.samples.length > 2 && p.t - g.samples[0].t > 100) g.samples.shift();
    // Content follows the finger: the finger going up scrolls the page down.
    this.scrollBy(g.remote, -dx, -dy, p.t, out);
  }

  /** Scrolls by local px of content travel at a picture point. */
  private scrollBy(at: Point, dx: number, dy: number, t: number, out: TouchAction[], limit = Infinity): number {
    const c = this.carry;
    if (t - c.at > TOUCH.CARRY_MS) {
      c.x = c.y = 0;
      c.fine = { x: 0, y: 0 };
    }
    c.at = t;
    if (this.fineScroll) {
      c.fine.x += dx / this.view.s;
      c.fine.y += dy / this.view.s;
      const ix = Math.trunc(c.fine.x);
      const iy = Math.trunc(c.fine.y);
      if (ix || iy) {
        c.fine.x -= ix;
        c.fine.y -= iy;
        out.push({ type: "scroll", x: at.x, y: at.y, dx: ix, dy: iy });
      }
      return 0;
    }
    const step = this.notchLocal();
    c.x += dx;
    c.y += dy;
    let nx = Math.trunc(c.x / step);
    let ny = Math.trunc(c.y / step);
    const room = Math.max(0, limit);
    if (Math.abs(nx) + Math.abs(ny) > room) {
      ny = Math.sign(ny) * Math.min(Math.abs(ny), room);
      nx = Math.sign(nx) * Math.min(Math.abs(nx), room - Math.abs(ny));
    }
    if (nx || ny) {
      c.x -= nx * step;
      c.y -= ny * step;
      out.push({ type: "wheel", x: at.x, y: at.y, dx: nx, dy: ny });
    }
    return Math.abs(nx) + Math.abs(ny);
  }

  private endScroll(g: Extract<One, { kind: "scroll" }>, t: number, out: TouchAction[]): void {
    const first = g.samples[0];
    const last = g.samples[g.samples.length - 1];
    const dt = last && first ? Math.max(1, last.t - first.t) : 1;
    // Only a finger still moving at the lift flicks (one that stopped before lifting does not).
    const fresh = last && t - last.t < 60;
    const vx = fresh && first ? (last.x - first.x) / dt : 0;
    const vy = fresh && first ? (last.y - first.y) / dt : 0;
    if (Math.hypot(vx, vy) >= TOUCH.FLING_MIN) {
      this.fling = { vx: -vx, vy: -vy, at: t, start: t, notches: 0, remote: g.remote };
      return;
    }
    this.settle(g.remote, t, out);
  }

  /** At the end of a scroll, travel past half a notch sends that notch: a short deliberate drag scrolls. */
  private settle(at: Point, t: number, out: TouchAction[]): void {
    if (this.fineScroll) return;
    const step = this.notchLocal();
    const c = this.carry;
    const nx = Math.abs(c.x) >= step / 2 ? Math.sign(c.x) : 0;
    const ny = Math.abs(c.y) >= step / 2 ? Math.sign(c.y) : 0;
    if (nx || ny) {
      c.x -= nx * step;
      c.y -= ny * step;
      c.at = t;
      out.push({ type: "wheel", x: at.x, y: at.y, dx: nx, dy: ny });
    }
  }

  private flingStep(t: number, out: TouchAction[]): void {
    const f = this.fling!;
    const dt = Math.max(0, t - f.at);
    if (dt <= 0) return;
    const decay = Math.exp(-dt / TOUCH.FLING_TAU);
    // Distance travelled over dt as velocity decays exponentially.
    const k = TOUCH.FLING_TAU * (1 - decay);
    const sent = this.scrollBy(f.remote, f.vx * k, f.vy * k, t, out, TOUCH.FLING_MAX_NOTCHES - f.notches);
    f.notches += sent;
    f.vx *= decay;
    f.vy *= decay;
    f.at = t;
    if (Math.hypot(f.vx, f.vy) < TOUCH.FLING_STOP || t - f.start >= TOUCH.FLING_MAX_MS || f.notches >= TOUCH.FLING_MAX_NOTCHES) {
      this.fling = null;
      this.carry.x = this.carry.y = 0;
    }
  }

  /** Whether a flick is still scrolling. */
  get flinging(): boolean {
    return this.fling !== null;
  }

  private stopFling(): void {
    if (!this.fling) return;
    this.fling = null;
    this.carry.x = this.carry.y = 0;
  }

  // ---- two fingers ----------------------------------------------------------------

  private moveTwo(g: Extract<Gesture, { kind: "two" }>, out: TouchAction[]): void {
    const a = this.touches.get(g.ids[0]);
    const b = this.touches.get(g.ids[1]);
    if (!a || !b) return;
    const s = { dist: Math.hypot(a.x - b.x, a.y - b.y), c: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } };
    if (!g.decided && (Math.abs(s.dist - g.start.dist) > TOUCH.TWO_PINCH || Math.hypot(s.c.x - g.start.c.x, s.c.y - g.start.c.y) > TOUCH.TWO_SLOP)) {
      // Decided: the view takes all the fingers' travel since they went down.
      g.decided = true;
      g.last = g.start;
    }
    if (g.decided) {
      this.zoomAt(g.scale * (s.dist / Math.max(1, g.start.dist)), s.c);
      this.view.tx += s.c.x - g.last.c.x;
      this.view.ty += s.c.y - g.last.c.y;
      // Panning keeps the pinch's anchor: the scale is applied around the current centre, the pan by its travel.
      g.scale = this.view.s;
      g.start = { dist: s.dist, c: s.c };
      this.clamp();
      out.push({ type: "view" });
    }
    g.last = s;
  }
}

/** A finger's speed in local px/ms, from the time between samples (never per event: touch rates differ). */
function speedOf(a: Sample, b: Sample, previous: number): number {
  const dt = b.t - a.t;
  if (dt <= 0) return previous;
  const v = Math.hypot(b.x - a.x, b.y - a.y) / dt;
  // Smoothed over about 24 ms.
  const k = Math.min(1, dt / 24);
  return previous + (v - previous) * k;
}
