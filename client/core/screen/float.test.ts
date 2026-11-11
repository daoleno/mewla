import { describe, expect, test } from "bun:test";
import type { PhoneStatus } from "../phone/status";
import {
  DEFAULT_FLOAT_CORNER,
  FLOAT_DEFAULT,
  FLOAT_LARGE,
  FLOAT_WIDE,
  FLOAT_WIDE_LARGE,
  clampFloatFraction,
  displayShown,
  displaySource,
  floatOrigin,
  floatSize,
  floatSources,
  macSource,
  nearestFloatCorner,
  parseFloatCorner,
  parseFloatFraction,
  phoneOwner,
  phoneSource,
  pickSource,
  resizedFloatWidth,
  routeOwner,
  screenTarget,
  toggledFloatFraction,
} from "./float";

// Ported from the Expo app's screenFloatModel.test.ts and floatGeometry.test.ts.

const workers = [
  { id: "%1", name: "Writer" },
  { id: "%2", name: "Coder" },
  { id: "%3", name: "Coder" },
];
const phoneStatus = { enabled: true, supported: true, paired: true, developer_options: true, wireless_debugging: true, wifi: true, helper: true, state: "ready", w: 720, h: 1600, agent: "Writer", handoff: null, self_healing: true } as unknown as PhoneStatus;
const display = { id: "%1", width: 1280, height: 800, control: "agent" as const, apps: 1 };
const page = { owner: null, phonePage: false, screenPage: false };

describe("route owner", () => {
  test("a Session is its Worker, a Work file the Session it was written in, other pages have none", () => {
    expect(routeOwner({ name: "session", workerId: "%152" }, () => undefined)).toBe("%152");
    expect(routeOwner({ name: "workFile", id: "abc" }, (id) => (id === "abc" ? "%7" : undefined))).toBe("%7");
    expect(routeOwner({ name: "workFile", id: "none" }, () => undefined)).toBeNull();
    expect(routeOwner({ name: "sessions" }, () => "%1")).toBeNull();
    expect(routeOwner({ name: "settings-phone" }, () => "%1")).toBeNull();
    expect(routeOwner(null, () => "%1")).toBeNull();
  });
});

describe("phone source", () => {
  test("is the owner when its name or id matches one Worker, ownerless otherwise", () => {
    expect(phoneOwner("%2", workers)).toBe("%2");
    expect(phoneOwner("Writer", workers)).toBe("%1");
    expect(phoneOwner("Coder", workers)).toBeNull();
    expect(phoneOwner("An agent", workers)).toBeNull();
  });
  test("exists only while an agent works the phone and its screen has a size", () => {
    expect(phoneSource(phoneStatus, workers)?.owner).toBe("%1");
    expect(phoneSource({ ...phoneStatus, agent: null }, workers)).toBeNull();
    expect(phoneSource({ ...phoneStatus, state: "stopped" }, workers)).toBeNull();
    expect(phoneSource({ ...phoneStatus, w: 0, h: 0 }, workers)).toBeNull();
    expect(phoneSource({ ...phoneStatus, enabled: false }, workers)).toBeNull();
    expect(phoneSource(null, workers)).toBeNull();
  });
});

describe("display shown", () => {
  const run = { running: true, shown: false, holding: false };
  test("appears while its Worker runs and used it a moment ago", () => {
    expect(displayShown({ idle_s: 5 }, run)).toBe(true);
    expect(displayShown({ idle_s: 61 }, run)).toBe(false);
    expect(displayShown({}, run)).toBe(true);
    expect(displayShown({ idle_s: 5 }, { ...run, running: false })).toBe(false);
  });
  test("stays up between actions while the Worker runs, not for ever", () => {
    expect(displayShown({ idle_s: 300 }, { ...run, shown: true })).toBe(true);
    expect(displayShown({ idle_s: 20 * 60 }, { ...run, shown: true })).toBe(false);
    expect(displayShown({ idle_s: 300 }, { ...run, shown: true, running: false })).toBe(false);
  });
  test("a person holding it keeps it up; a Stop takes it away", () => {
    expect(displayShown({ idle_s: 3600 }, { running: false, shown: true, holding: true })).toBe(true);
    expect(displayShown({ stopped: true, idle_s: 0 }, { ...run, holding: true })).toBe(false);
  });
});

describe("which screens the window may show", () => {
  const phone = phoneSource(phoneStatus, workers)!;
  const mine = displaySource(display, "Writer");
  const other = displaySource({ ...display, id: "%2" });
  const mac = macSource({ worker: "%2", since: "" } as never);
  const all = [phone, mine, other, mac];

  test("a Session shows its Worker's screens only", () => {
    expect(floatSources(all, { ...page, owner: "%1" }).map((s) => s.key)).toEqual(["phone", "display:%1"]);
    expect(floatSources(all, { ...page, owner: "%2" }).map((s) => s.key)).toEqual(["display:%2", "mac:%2"]);
  });
  test("a screen of an unknown Worker shows in every Session", () => {
    const loose = { ...phone, owner: null };
    expect(floatSources([loose], { ...page, owner: "%9" })).toHaveLength(1);
  });
  test("a page that belongs to no Worker shows them all", () => {
    expect(floatSources(all, page)).toHaveLength(4);
  });
  test("a screen hides on its own full view", () => {
    expect(floatSources(all, { ...page, phonePage: true }).map((s) => s.key)).toEqual(["display:%1", "display:%2", "mac:%2"]);
    expect(floatSources(all, { ...page, screenPage: true }).map((s) => s.key)).toEqual(["phone"]);
  });
  test("the picked one shows while it is there, else the first", () => {
    expect(pickSource(all, "display:%2")?.key).toBe("display:%2");
    expect(pickSource(all, "gone")?.key).toBe("phone");
    expect(pickSource([], null)).toBeNull();
  });
  test("a display is named by its Worker, else its id; a Mac by its agent", () => {
    expect(mine.title).toBe("Writer");
    expect(other.title).toBe("%2");
    expect(macSource({ worker: "%2", name: "Coder", since: "" } as never).title).toBe("Coder");
    expect(mac.title).toBe("An agent");
  });
});

describe("what opens a screen", () => {
  test("a display opens its viewer route; a Mac only watches that Worker, it never takes the screen", () => {
    expect(screenTarget(displaySource(display))).toEqual({ watch: false, route: { path: "/display/viewer", purpose: "mewla-display-view", params: { worker: "%1" } } });
    expect(screenTarget(macSource({ worker: "%2", since: "" } as never))).toEqual({ watch: true, worker: "%2" });
    expect(screenTarget({ kind: "phone", owner: "%1" })).toBeNull();
  });
});

describe("float geometry", () => {
  const area = { left: 0, top: 100, right: 400, bottom: 700 };
  const size = { width: 80, height: 180 };

  test("sits in the four corners of the free area", () => {
    expect(floatOrigin("top-left", area, size, 12)).toEqual({ x: 12, y: 112 });
    expect(floatOrigin("top-right", area, size, 12)).toEqual({ x: 308, y: 112 });
    expect(floatOrigin("bottom-left", area, size, 12)).toEqual({ x: 12, y: 508 });
    expect(floatOrigin("bottom-right", area, size, 12)).toEqual({ x: 308, y: 508 });
  });

  test("a keyboard that shrinks the area moves the bottom corners up and never off the top", () => {
    const small = { ...area, bottom: 200 };
    expect(floatOrigin("bottom-right", small, size, 12).y).toBe(100);
    expect(floatOrigin("top-right", small, size, 12).y).toBe(112);
  });

  test("snaps to the corner nearest the float's centre", () => {
    expect(nearestFloatCorner({ x: 50, y: 150 }, area)).toBe("top-left");
    expect(nearestFloatCorner({ x: 350, y: 150 }, area)).toBe("top-right");
    expect(nearestFloatCorner({ x: 50, y: 650 }, area)).toBe("bottom-left");
    expect(nearestFloatCorner({ x: 350, y: 650 }, area)).toBe("bottom-right");
  });

  const window = { width: 411, height: 914 };
  const ratio = 720 / 1600;

  test("is the screen's exact shape, at 55% of the width by default", () => {
    const size = floatSize(ratio, FLOAT_DEFAULT, window);
    expect(size.width).toBeCloseTo(226.05, 2);
    expect(size.width / size.height).toBeCloseTo(ratio, 10);
    expect(size.height).toBeLessThan(0.6 * window.height);
  });

  test("the default never covers more than 60% of the height, even on a short wide display", () => {
    const wide = floatSize(0.8, FLOAT_DEFAULT, { width: 411, height: 500 });
    expect(wide.height).toBeLessThanOrEqual(0.6 * 500 + 1e-9);
    expect(wide.width / wide.height).toBeCloseTo(0.8, 10);
  });

  test("a size the person chose may be taller, up to 85% of the height", () => {
    const large = floatSize(ratio, FLOAT_LARGE, window);
    expect(large.width).toBeCloseTo(328.8, 1);
    expect(large.height).toBeCloseTo(730.7, 1);
    expect(large.height).toBeLessThanOrEqual(0.85 * window.height);
    expect(floatSize(ratio, 0.9, { width: 411, height: 700 }).height).toBeLessThanOrEqual(0.85 * 700 + 1e-9);
  });

  test("the least width is 30% of the window, however little room there is", () => {
    const tight = floatSize(ratio, FLOAT_DEFAULT, window, 100);
    expect(tight.width).toBeCloseTo(0.3 * window.width, 5);
    expect(tight.width / tight.height).toBeCloseTo(ratio, 10);
  });

  test("a keyboard gives height first and keeps the shape", () => {
    const size = floatSize(ratio, FLOAT_DEFAULT, window, 400);
    expect(size.height).toBeCloseTo(400, 5);
    expect(size.width / size.height).toBeCloseTo(ratio, 10);
  });

  test("a display with no size yet falls back to a phone's shape", () => {
    expect(floatSize(0, FLOAT_DEFAULT, window).width / floatSize(0, FLOAT_DEFAULT, window).height).toBeCloseTo(9 / 20, 10);
  });

  test("dragging the inner corner outward grows it along the diagonal, for every corner", () => {
    expect(resizedFloatWidth(200, ratio, { x: 0, y: 0 }, "top-left")).toBe(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: 0 }, "top-left")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 0, y: 40 }, "top-left")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: -30, y: -40 }, "top-left")).toBeLessThan(200);
    expect(resizedFloatWidth(200, ratio, { x: -30, y: 0 }, "top-right")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: 0 }, "top-right")).toBeLessThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 0, y: -40 }, "bottom-right")).toBeGreaterThan(200);
    expect(resizedFloatWidth(200, ratio, { x: 30, y: -40 }, "bottom-left")).toBeGreaterThan(200);
  });

  test("a drag along the diagonal moves the corner exactly as far as the width says", () => {
    expect(resizedFloatWidth(200, ratio, { x: 20, y: 20 / ratio }, "top-left")).toBeCloseTo(220, 8);
  });

  test("a double tap goes between the default and the large size", () => {
    expect(toggledFloatFraction(FLOAT_DEFAULT)).toBe(FLOAT_LARGE);
    expect(toggledFloatFraction(FLOAT_LARGE)).toBe(FLOAT_DEFAULT);
    expect(toggledFloatFraction(0.4)).toBe(FLOAT_DEFAULT);
  });

  test("remembers a size only in range, and clamps one to 30%-90%", () => {
    expect(parseFloatFraction("0.7")).toBe(0.7);
    expect(parseFloatFraction("0.1")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction("2")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction("wide")).toBe(FLOAT_DEFAULT);
    expect(parseFloatFraction(null)).toBe(FLOAT_DEFAULT);
    expect(clampFloatFraction(0.1)).toBe(0.3);
    expect(clampFloatFraction(1.4)).toBe(0.9);
    expect(clampFloatFraction(0.5)).toBe(0.5);
  });

  test("remembers only a corner it knows", () => {
    expect(parseFloatCorner("bottom-left")).toBe("bottom-left");
    expect(parseFloatCorner("middle")).toBe("bottom-right");
    expect(parseFloatCorner(null)).toBe("bottom-right");
  });

  test("starts in the bottom-right corner, above the composer, so most of the chat stays readable", () => {
    expect(DEFAULT_FLOAT_CORNER).toBe("bottom-right");
    expect(floatOrigin(DEFAULT_FLOAT_CORNER, { left: 0, top: 100, right: 400, bottom: 700 }, { width: 220, height: 480 }, 12)).toEqual({ x: 168, y: 208 });
  });

  test("on a wide window the default and large sizes stay corner windows; a dragged size is not capped", () => {
    const wide = { width: 1400, height: 900 };
    expect(floatSize(1.6, FLOAT_DEFAULT, wide).width).toBe(FLOAT_WIDE);
    expect(floatSize(1.6, FLOAT_LARGE, wide).width).toBe(FLOAT_WIDE_LARGE);
    expect(floatSize(1.6, 0.5, wide).width).toBeCloseTo(700, 5);
  });
});
