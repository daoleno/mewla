/**
 * The Mewla icon set: original drawings on a 24 grid, stroked at 1.5 with
 * round caps and joins, soft corners on every box and bend, outline only.
 * Filled shapes appear only where the fill carries state (the `*-fill`
 * notices, a selected radio, the active dot); their marks are cut out as
 * even-odd holes so the glyph works on any ground.
 */
export interface MewlaGlyph {
  /** Stroked at 1.5 with round caps and joins. */
  strokes: readonly string[];
  /** Filled solid with the even-odd rule, for dots and state shapes. */
  fills?: readonly string[];
}

type Point = readonly [number, number];

/** Two decimals, no exponent, no negative zero. */
function n(value: number): string {
  const rounded = Math.round(value * 100) / 100;
  return rounded === 0 ? "0" : String(rounded);
}

function pt([x, y]: Point): string {
  return `${n(x)},${n(y)}`;
}

function circle(cx: number, cy: number, r: number): string {
  return `M${n(cx - r)},${n(cy)}a${n(r)},${n(r)} 0 1 0 ${n(r * 2)},0a${n(r)},${n(r)} 0 1 0 ${n(-r * 2)},0Z`;
}

/** A rounded box; corners are quarter circles of radius `r`. */
function box(x: number, y: number, w: number, h: number, r: number): string {
  const a = `${n(r)},${n(r)} 0 0 1`;
  return (
    `M${n(x + r)},${n(y)}H${n(x + w - r)}A${a} ${n(x + w)},${n(y + r)}V${n(y + h - r)}` +
    `A${a} ${n(x + w - r)},${n(y + h)}H${n(x + r)}A${a} ${n(x)},${n(y + h - r)}V${n(y + r)}A${a} ${n(x + r)},${n(y)}Z`
  );
}

/** A line through `points` whose bends are eased by up to `r`. */
function soft(points: readonly Point[], r = 1.5, closed = false): string {
  const pts = closed ? [...points, points[0], points[1]] : points;
  let d = `M${pt(closed ? toward(pts[0], pts[1], Math.min(r, dist(pts[0], pts[1]) / 2)) : pts[0])}`;
  for (let i = 1; i < pts.length - 1; i += 1) {
    const [prev, at, next] = [pts[i - 1], pts[i], pts[i + 1]];
    const inCut = Math.min(r, dist(prev, at) / 2);
    const outCut = Math.min(r, dist(at, next) / 2);
    d += `L${pt(toward(at, prev, inCut))}Q${pt(at)} ${pt(toward(at, next, outCut))}`;
  }
  return closed ? `${d}Z` : `${d}L${pt(pts[pts.length - 1])}`;
}

function dist(a: Point, b: Point): number {
  return Math.hypot(b[0] - a[0], b[1] - a[1]);
}

function toward(from: Point, to: Point, length: number): Point {
  const d = dist(from, to);
  return [from[0] + ((to[0] - from[0]) * length) / d, from[1] + ((to[1] - from[1]) * length) / d];
}

function dot(cx: number, cy: number, r = 1): string {
  return circle(cx, cy, r);
}

/** The outline of a round-capped stroke from a to b, half-width h. */
function capsule(a: Point, b: Point, h: number): string {
  const [dx, dy] = unit(a, b);
  const [nx, ny] = [-dy * h, dx * h];
  return (
    `M${pt([a[0] + nx, a[1] + ny])}L${pt([b[0] + nx, b[1] + ny])}A${n(h)},${n(h)} 0 0 0 ${pt([b[0] - nx, b[1] - ny])}` +
    `L${pt([a[0] - nx, a[1] - ny])}A${n(h)},${n(h)} 0 0 0 ${pt([a[0] + nx, a[1] + ny])}Z`
  );
}

/** The outline of a round-capped, round-joined two-segment stroke. */
function bendOutline(p0: Point, p1: Point, p2: Point, h: number): string {
  const d1 = unit(p0, p1);
  const d2 = unit(p1, p2);
  const n1: Point = [-d1[1], d1[0]];
  const n2: Point = [-d2[1], d2[0]];
  const off = (p: Point, v: Point, s: number): Point => [p[0] + v[0] * h * s, p[1] + v[1] * h * s];
  const joint = (s: number): string => {
    const turnsToward = s * (n1[0] * d2[0] + n1[1] * d2[1]) > 0;
    if (!turnsToward) return `L${pt(off(p1, n1, s))}A${n(h)},${n(h)} 0 0 0 ${pt(off(p1, n2, s))}`;
    const a = off(p0, n1, s);
    const c = off(p1, n2, s);
    const t = cross([c[0] - a[0], c[1] - a[1]], d2) / cross(d1, d2);
    return `L${pt([a[0] + d1[0] * t, a[1] + d1[1] * t])}`;
  };
  const jointBack = (s: number): string => {
    const turnsToward = s * (n1[0] * d2[0] + n1[1] * d2[1]) > 0;
    if (!turnsToward) return `L${pt(off(p1, n2, s))}A${n(h)},${n(h)} 0 0 1 ${pt(off(p1, n1, s))}`;
    return joint(s);
  };
  return (
    `M${pt(off(p0, n1, 1))}${joint(1)}L${pt(off(p2, n2, 1))}A${n(h)},${n(h)} 0 0 0 ${pt(off(p2, n2, -1))}` +
    `${jointBack(-1)}L${pt(off(p0, n1, -1))}A${n(h)},${n(h)} 0 0 0 ${pt(off(p0, n1, 1))}Z`
  );
}

/** The outline of an X: two round-capped strokes of half-width h crossing at c. */
function crossOutline(c: Point, arm: number, h: number): string {
  const dirs: Point[] = [45, 135, 225, 315].map((deg) => {
    const a = (deg * Math.PI) / 180;
    return [Math.cos(a), Math.sin(a)];
  });
  let d = "";
  dirs.forEach((u, k) => {
    const prev = dirs[(k + 3) % 4];
    const corner: Point = [c[0] + (u[0] + prev[0]) * h, c[1] + (u[1] + prev[1]) * h];
    const end: Point = [c[0] + u[0] * arm, c[1] + u[1] * arm];
    const nrm: Point = [-u[1] * h, u[0] * h];
    d += `${k === 0 ? "M" : "L"}${pt(corner)}L${pt([end[0] - nrm[0], end[1] - nrm[1]])}`;
    d += `A${n(h)},${n(h)} 0 0 1 ${pt([end[0] + nrm[0], end[1] + nrm[1]])}`;
  });
  return `${d}Z`;
}

function unit(a: Point, b: Point): Point {
  const d = dist(a, b);
  return [(b[0] - a[0]) / d, (b[1] - a[1]) / d];
}

function cross(a: Point, b: Point): number {
  return a[0] * b[1] - a[1] * b[0];
}

/** Six soft teeth around a hub. */
function gear(): string {
  const points: Point[] = [];
  const at = (r: number, deg: number): Point => {
    const a = ((deg - 90) * Math.PI) / 180;
    return [12 + r * Math.cos(a), 12 + r * Math.sin(a)];
  };
  for (let k = 0; k < 6; k += 1) {
    const mid = k * 60;
    points.push(at(6.9, mid - 21), at(9.35, mid - 12), at(9.35, mid + 12), at(6.9, mid + 21));
  }
  return soft(points, 1.3, true);
}

/** Eight short rays around a centre. */
function rays(cx: number, cy: number, from: number, to: number): string[] {
  return Array.from({ length: 8 }, (_, k) => {
    const a = (k * Math.PI) / 4;
    const [c, s] = [Math.cos(a), Math.sin(a)];
    return `M${pt([cx + c * from, cy + s * from])}L${pt([cx + c * to, cy + s * to])}`;
  });
}

const HOLE = 0.8;
const RING = circle(12, 12, 9.25);
const CLOUD = "M7,18.5A4.5,4.5 0 0 1 6.37,9.54A6,6 0 0 1 17.63,9.54A4.5,4.5 0 0 1 17,18.5Z";
const CLOUD_OPEN = "M8,18.5H7A4.5,4.5 0 0 1 6.37,9.54A6,6 0 0 1 17.63,9.54A4.5,4.5 0 0 1 17,18.5H16";
const CHECK: Point[] = [
  [8.25, 12.4],
  [10.75, 14.9],
  [15.75, 9.4],
];
const STACK = [
  soft(
    [
      [12, 3.5],
      [20.75, 8],
      [12, 12.5],
      [3.25, 8],
    ],
    1.5,
    true,
  ),
  soft([
    [3.25, 12.25],
    [12, 16.75],
    [20.75, 12.25],
  ]),
  soft([
    [3.25, 16.25],
    [12, 20.75],
    [20.75, 16.25],
  ]),
];
const TERMINAL = [
  box(2.75, 3.75, 18.5, 16.5, 4),
  soft(
    [
      [7.25, 9.25],
      [10, 12],
      [7.25, 14.75],
    ],
    1,
  ),
  "M12.75,14.75H16.75",
];

function ear(prev: Point, tip: Point, next: Point, r: number): string {
  return `L${pt(toward(tip, prev, r))}Q${pt(tip)} ${pt(toward(tip, next, r))}`;
}

/** A round bubble with a tail low on the left, optionally with cat ears. */
function bubble(withEars: boolean): string {
  if (!withEars) {
    return `M3.86,15.2A8.75,8.75 0 1 1 6.02,18.54${ear([6.02, 18.54], [3.5, 20.75], [3.86, 15.2], 1)}Z`;
  }
  const c: Point = [12, 13.25];
  const r = 7.75;
  const on = (deg: number): Point => {
    const a = (deg * Math.PI) / 180;
    return [c[0] + r * Math.cos(a), c[1] + r * Math.sin(a)];
  };
  const [ear1a, ear1b, ear2a, ear2b] = [on(-110), on(-70), on(-30), on(-150)];
  const [tailA, tailB] = [on(135), on(162)];
  const arc = (to: Point, large: 0 | 1) => `A${n(r)},${n(r)} 0 ${large} 1 ${pt(to)}`;
  return (
    `M${pt(ear1a)}${arc(ear1b, 0)}${ear(ear1b, [17.5, 3.75], ear2a, 1.1)}L${pt(ear2a)}` +
    `${arc(tailA, 0)}${ear(tailA, [3.75, 21.25], tailB, 1)}L${pt(tailB)}` +
    `${arc(ear2b, 0)}${ear(ear2b, [6.5, 3.75], ear1a, 1.1)}Z`
  );
}

export const MEWLA_GLYPHS = {
  add: { strokes: ["M12,4.75V19.25", "M4.75,12H19.25"] },
  "add-circle": { strokes: [RING, "M12,8.25V15.75", "M8.25,12H15.75"] },
  alert: { strokes: ["M12,4.75V14.25"], fills: [dot(12, 18.75, 1.1)] },
  "alert-circle": { strokes: [RING, "M12,7.75V12.75"], fills: [dot(12, 16.1)] },
  "alert-circle-fill": {
    strokes: [],
    fills: [circle(12, 12, 10) + capsule([12, 7.5], [12, 12.75], HOLE) + dot(12, 16.25, 1.1)],
  },
  apps: {
    strokes: [box(3, 3, 7.75, 7.75, 2.75), box(13.25, 3, 7.75, 7.75, 2.75), box(3, 13.25, 7.75, 7.75, 2.75), box(13.25, 13.25, 7.75, 7.75, 2.75)],
  },
  "arrow-down": {
    strokes: [
      "M12,4.75V19",
      soft(
        [
          [6.25, 13.25],
          [12, 19],
          [17.75, 13.25],
        ],
        1,
      ),
    ],
  },
  "arrow-forward": {
    strokes: [
      "M4.75,12H19",
      soft(
        [
          [13.25, 6.25],
          [19, 12],
          [13.25, 17.75],
        ],
        1,
      ),
    ],
  },
  "arrow-up": {
    strokes: [
      "M12,19.25V5",
      soft(
        [
          [6.25, 10.75],
          [12, 5],
          [17.75, 10.75],
        ],
        1,
      ),
    ],
  },
  at: {
    strokes: [circle(12, 12, 3.75), "M15.75,8.25V13.5a2.5,2.5 0 0 0 5,0V12a8.75,8.75 0 1 0 -3.6,7.08"],
  },
  attach: {
    strokes: ["M19.25,11.25L12.5,18a4.6,4.6 0 0 1 -6.5,-6.5L13.25,4.25a3.06,3.06 0 0 1 4.33,4.33L10.4,15.75a1.53,1.53 0 0 1 -2.17,-2.17L14.75,7.1"],
  },
  book: {
    strokes: [
      "M12,6.75C9.9,5.15 7,4.6 4.25,4.85A1.4,1.4 0 0 0 3,6.25V17.4a1.4,1.4 0 0 0 1.5,1.4C7.15,18.6 9.95,19.15 12,20.6C14.05,19.15 16.85,18.6 19.5,18.8a1.4,1.4 0 0 0 1.5,-1.4V6.25a1.4,1.4 0 0 0 -1.25,-1.4C17,4.6 14.1,5.15 12,6.75Z",
      "M12,6.75V20.6",
    ],
  },
  brain: {
    strokes: [bubble(true)],
    fills: [dot(8.75, 13.5), dot(12, 13.5), dot(15.25, 13.5)],
  },
  browser: {
    strokes: [RING, "M12,2.75C9.6,5.3 8.4,8.4 8.4,12S9.6,18.7 12,21.25C14.4,18.7 15.6,15.6 15.6,12S14.4,5.3 12,2.75Z", "M2.75,12H21.25"],
  },
  bug: {
    strokes: [
      box(7.25, 8, 9.5, 12.75, 4.75),
      "M9.25,8.1V7.5a2.75,2.75 0 0 1 5.5,0V8.1",
      "M12,12.5V20.5",
      "M7.25,12.75H3.75",
      "M16.75,12.75H20.25",
      "M7.4,16.75L4.5,18.5",
      "M16.6,16.75L19.5,18.5",
      "M7.6,9.4L5,7.25",
      "M16.4,9.4L19,7.25",
    ],
  },
  bulb: {
    strokes: [
      "M9,17V15.9C9,14.6 8.3,13.65 7.45,12.8A6.25,6.25 0 1 1 16.55,12.8C15.7,13.65 15,14.6 15,15.9V17Z",
      "M10,20.75H14",
    ],
  },
  calendar: {
    strokes: [box(3, 4.75, 18, 16.25, 4), "M3,9.75H21", "M8,2.75V6.5", "M16,2.75V6.5"],
    fills: [dot(8, 13.5), dot(12, 13.5), dot(16, 13.5), dot(8, 17), dot(12, 17)],
  },
  "calendar-blank": {
    strokes: [box(3, 4.75, 18, 16.25, 4), "M3,9.75H21", "M8,2.75V6.5", "M16,2.75V6.5"],
  },
  "calendar-fill": {
    strokes: ["M8,2.75V5", "M16,2.75V5"],
    fills: [
      box(2.5, 4.25, 19, 17.25, 4.25) +
        capsule([5.25, 9.5], [18.75, 9.5], 0.7) +
        dot(8, 13.5, 1.15) +
        dot(12, 13.5, 1.15) +
        dot(16, 13.5, 1.15) +
        dot(8, 17, 1.15) +
        dot(12, 17, 1.15),
    ],
  },
  camera: {
    strokes: [
      "M2.75,10.5a4,4 0 0 1 4,-4H7.9L9.2,4.6a1.6,1.6 0 0 1 1.33,-.72h2.94a1.6,1.6 0 0 1 1.33,.72L16.1,6.5h1.15a4,4 0 0 1 4,4v5.75a4,4 0 0 1 -4,4H6.75a4,4 0 0 1 -4,-4Z",
      circle(12, 13.25, 3.5),
    ],
  },
  chat: { strokes: [bubble(false)] },
  "chat-dots": {
    strokes: [bubble(false)],
    fills: [dot(8.25, 12), dot(12, 12), dot(15.75, 12)],
  },
  chatbox: {
    strokes: ["M7,3.75H17a4,4 0 0 1 4,4V14a4,4 0 0 1 -4,4H14.6L12.8,20.2a1,1 0 0 1 -1.6,0L9.4,18H7a4,4 0 0 1 -4,-4V7.75a4,4 0 0 1 4,-4Z"],
  },
  "chatbox-dots": {
    strokes: ["M7,3.75H17a4,4 0 0 1 4,4V14a4,4 0 0 1 -4,4H14.6L12.8,20.2a1,1 0 0 1 -1.6,0L9.4,18H7a4,4 0 0 1 -4,-4V7.75a4,4 0 0 1 4,-4Z"],
    fills: [dot(8.25, 10.9), dot(12, 10.9), dot(15.75, 10.9)],
  },
  check: {
    strokes: [
      soft(
        [
          [4.75, 12.75],
          [9.25, 17.25],
          [19.25, 6.75],
        ],
        1,
      ),
    ],
  },
  "check-circle": { strokes: [RING, soft(CHECK, 0.8)] },
  "check-circle-fill": { strokes: [], fills: [circle(12, 12, 10) + bendOutline(CHECK[0], CHECK[1], CHECK[2], HOLE)] },
  checkbox: { strokes: [box(3, 3, 18, 18, 5), soft(CHECK, 0.8)] },
  checks: {
    strokes: [
      soft(
        [
          [2.75, 12.75],
          [6.75, 16.75],
          [15.25, 7.5],
        ],
        1,
      ),
      soft(
        [
          [11.25, 15.25],
          [12.75, 16.75],
          [21.25, 7.5],
        ],
        1,
      ),
    ],
  },
  "chevron-down": {
    strokes: [
      soft(
        [
          [6, 9.25],
          [12, 15.25],
          [18, 9.25],
        ],
        1.6,
      ),
    ],
  },
  "chevron-left": {
    strokes: [
      soft(
        [
          [14.75, 6],
          [8.75, 12],
          [14.75, 18],
        ],
        1.6,
      ),
    ],
  },
  "chevron-right": {
    strokes: [
      soft(
        [
          [9.25, 6],
          [15.25, 12],
          [9.25, 18],
        ],
        1.6,
      ),
    ],
  },
  "chevron-up": {
    strokes: [
      soft(
        [
          [6, 14.75],
          [12, 8.75],
          [18, 14.75],
        ],
        1.6,
      ),
    ],
  },
  chip: {
    strokes: [
      box(5.25, 5.25, 13.5, 13.5, 3.5),
      box(9.25, 9.25, 5.5, 5.5, 1.5),
      "M9.25,2.75V5.25",
      "M14.75,2.75V5.25",
      "M9.25,18.75V21.25",
      "M14.75,18.75V21.25",
      "M2.75,9.25H5.25",
      "M2.75,14.75H5.25",
      "M18.75,9.25H21.25",
      "M18.75,14.75H21.25",
    ],
  },
  circle: { strokes: [RING] },
  clipboard: {
    strokes: [
      "M8.5,4.75H7.75a3.25,3.25 0 0 0 -3.25,3.25V18a3.25,3.25 0 0 0 3.25,3.25h8.5A3.25,3.25 0 0 0 19.5,18V8a3.25,3.25 0 0 0 -3.25,-3.25H15.5",
      box(8.5, 2.75, 7, 4, 1.75),
    ],
  },
  close: { strokes: ["M6.25,6.25L17.75,17.75", "M17.75,6.25L6.25,17.75"] },
  "close-circle": { strokes: [RING, "M9.25,9.25L14.75,14.75", "M14.75,9.25L9.25,14.75"] },
  "close-circle-fill": { strokes: [], fills: [circle(12, 12, 10) + crossOutline([12, 12], 3.9, HOLE)] },
  "cloud-download": {
    strokes: [
      CLOUD_OPEN,
      "M12,12V20.75",
      soft(
        [
          [9.75, 18.5],
          [12, 20.75],
          [14.25, 18.5],
        ],
        0.8,
      ),
    ],
  },
  "cloud-offline": { strokes: [CLOUD, "M3.75,3.75L20.25,20.25"] },
  "cloud-upload": {
    strokes: [
      CLOUD_OPEN,
      "M12,20.75V12",
      soft(
        [
          [9.75, 14.25],
          [12, 12],
          [14.25, 14.25],
        ],
        0.8,
      ),
    ],
  },
  code: {
    strokes: [
      soft(
        [
          [8, 7],
          [3, 12],
          [8, 17],
        ],
        1.2,
      ),
      soft(
        [
          [16, 7],
          [21, 12],
          [16, 17],
        ],
        1.2,
      ),
      "M13.75,5.25L10.25,18.75",
    ],
  },
  construct: {
    strokes: [
      "M14.6,3.25a5.4,5.4 0 0 0 -4.95,7.55L3.9,16.55a2.3,2.3 0 0 0 3.25,3.25L12.9,14.05a5.4,5.4 0 0 0 7.55,-4.95a.5,.5 0 0 0 -.85,-.35L17.6,10.75L13.95,10.05L13.25,6.4L15.25,4.4a.5,.5 0 0 0 -.35,-.85Z",
    ],
  },
  contrast: { strokes: [RING], fills: ["M12,5.75a6.25,6.25 0 0 1 0,12.5Z"] },
  copy: {
    strokes: [
      box(8.25, 8.25, 12.75, 12.75, 3.5),
      "M15.75,8.25V6.5a3.5,3.5 0 0 0 -3.5,-3.5H6.5A3.5,3.5 0 0 0 3,6.5v5.75a3.5,3.5 0 0 0 3.5,3.5H8.25",
    ],
  },
  cube: {
    strokes: [
      soft(
        [
          [12, 2.75],
          [20.25, 7.25],
          [20.25, 16.75],
          [12, 21.25],
          [3.75, 16.75],
          [3.75, 7.25],
        ],
        1.5,
        true,
      ),
      "M4.25,7.5L12,11.75L19.75,7.5",
      "M12,11.75V20.75",
    ],
  },
  cut: {
    strokes: [circle(6.25, 6.5, 2.75), circle(6.25, 17.5, 2.75), "M8.6,8.05L20.25,17", "M8.6,15.95L20.25,7"],
  },
  desktop: { strokes: [box(2.75, 3.5, 18.5, 13, 3.25), "M12,16.5V20.5", "M8,20.75H16"] },
  document: {
    strokes: [
      "M13.75,2.75H8.25a4,4 0 0 0 -4,4v10.5a4,4 0 0 0 4,4h7.5a4,4 0 0 0 4,-4V8.75Z",
      "M13.75,3V6.75a2,2 0 0 0 2,2H19.5",
    ],
  },
  "document-text": {
    strokes: [
      "M13.75,2.75H8.25a4,4 0 0 0 -4,4v10.5a4,4 0 0 0 4,4h7.5a4,4 0 0 0 4,-4V8.75Z",
      "M13.75,3V6.75a2,2 0 0 0 2,2H19.5",
      "M8.25,13H15.75",
      "M8.25,16.75H12.75",
    ],
  },
  dot: { strokes: [], fills: [circle(12, 12, 8)] },
  download: {
    strokes: [
      "M12,3.25V14.75",
      soft(
        [
          [7.75, 10.5],
          [12, 14.75],
          [16.25, 10.5],
        ],
        1,
      ),
      "M3.75,15.5V17a3.75,3.75 0 0 0 3.75,3.75h9A3.75,3.75 0 0 0 20.25,17V15.5",
    ],
  },
  edit: {
    strokes: [
      "M14.5,5.5L18.5,9.5",
      "M15.9,4.1a2.15,2.15 0 0 1 3.04,0l1,1a2.15,2.15 0 0 1 0,3.04L9.2,18.88a2.4,2.4 0 0 1 -1.32,.68L4.25,20.1a.3,.3 0 0 1 -.35,-.35l.54,-3.63a2.4,2.4 0 0 1 .68,-1.32Z",
    ],
  },
  enter: {
    strokes: [
      "M14.25,3.5H17a3.75,3.75 0 0 1 3.75,3.75v9.5A3.75,3.75 0 0 1 17,20.5H14.25",
      "M3.25,12H14.5",
      soft(
        [
          [10.75, 8.25],
          [14.5, 12],
          [10.75, 15.75],
        ],
        1,
      ),
    ],
  },
  exit: {
    strokes: [
      "M9.75,3.5H7a3.75,3.75 0 0 0 -3.75,3.75v9.5A3.75,3.75 0 0 0 7,20.5H9.75",
      "M9.5,12H20.75",
      soft(
        [
          [17, 8.25],
          [20.75, 12],
          [17, 15.75],
        ],
        1,
      ),
    ],
  },
  expand: {
    strokes: [
      soft(
        [
          [14.75, 3.25],
          [20.75, 3.25],
          [20.75, 9.25],
        ],
        1.5,
      ),
      "M20.25,3.75L14,10",
      soft(
        [
          [9.25, 20.75],
          [3.25, 20.75],
          [3.25, 14.75],
        ],
        1.5,
      ),
      "M3.75,20.25L10,14",
    ],
  },
  eye: {
    strokes: [
      "M2.75,12C4.9,7.75 8.1,5.5 12,5.5S19.1,7.75 21.25,12C19.1,16.25 15.9,18.5 12,18.5S4.9,16.25 2.75,12Z",
      circle(12, 12, 3),
    ],
  },
  flag: {
    strokes: [
      "M5,21.25V3.5",
      "M5,4.5C8,3 10.5,3.4 12.5,4.4S16.75,5.9 19.75,4.6V14.1C16.75,15.4 14.5,14.9 12.5,13.9S8,12.5 5,14",
    ],
  },
  flash: {
    strokes: [
      soft(
        [
          [13.75, 2.75],
          [4.75, 13.5],
          [11.5, 13.5],
          [10.25, 21.25],
          [19.25, 10.5],
          [12.5, 10.5],
        ],
        1.1,
        true,
      ),
    ],
  },
  flask: {
    strokes: [
      "M9,2.75H15",
      "M10,3V8.6L4.55,18a2.15,2.15 0 0 0 1.86,3.25H17.6A2.15,2.15 0 0 0 19.45,18L14,8.6V3",
      "M6.6,14.5H17.4",
    ],
  },
  folder: {
    strokes: [
      "M2.75,7a3.5,3.5 0 0 1 3.5,-3.5h2.6a2,2 0 0 1 1.6,.8l1.1,1.4a2,2 0 0 0 1.6,.8h4.6a3.5,3.5 0 0 1 3.5,3.5V17a3.5,3.5 0 0 1 -3.5,3.5H6.25A3.5,3.5 0 0 1 2.75,17Z",
    ],
  },
  "folder-open": {
    strokes: [
      "M3,17.25V7a3.5,3.5 0 0 1 3.5,-3.5h2.35a2,2 0 0 1 1.6,.8l1.1,1.4a2,2 0 0 0 1.6,.8h3.6a3,3 0 0 1 3,3v1",
      "M3,17.25L4.75,12.4A2.75,2.75 0 0 1 7.35,10.5H20.1a1.5,1.5 0 0 1 1.43,1.95L19.85,18a3.5,3.5 0 0 1 -3.35,2.5H6.25A3.25,3.25 0 0 1 3,17.25Z",
    ],
  },
  "git-branch": {
    strokes: [circle(6.25, 5.25, 2.25), circle(6.25, 18.75, 2.25), circle(17.75, 5.25, 2.25), "M6.25,7.5V16.5", "M17.75,7.5v.75a4,4 0 0 1 -4,4h-3.5a4,4 0 0 0 -4,4"],
  },
  "git-compare": {
    strokes: [
      circle(5.75, 5.25, 2.25),
      circle(18.25, 18.75, 2.25),
      "M18.25,16.5V9.5a4,4 0 0 0 -4,-4H10",
      soft(
        [
          [12.5, 3],
          [10, 5.5],
          [12.5, 8],
        ],
        0.8,
      ),
      "M5.75,7.5v7a4,4 0 0 0 4,4H14",
      soft(
        [
          [11.5, 16],
          [14, 18.5],
          [11.5, 21],
        ],
        0.8,
      ),
    ],
  },
  "git-network": {
    strokes: [circle(6, 5.25, 2.25), circle(18, 5.25, 2.25), circle(12, 18.75, 2.25), "M6,7.5V8.5a3,3 0 0 0 3,3h6a3,3 0 0 0 3,-3V7.5", "M12,11.5V16.5"],
  },
  hand: {
    strokes: [
      "M8,13.25V6.25a1.5,1.5 0 0 1 3,0V11.5",
      "M11,10.75V4.5a1.5,1.5 0 0 1 3,0v6.25",
      "M14,10.75V6a1.5,1.5 0 0 1 3,0v5",
      "M17,10.25a1.5,1.5 0 0 1 3,0V14a7.25,7.25 0 0 1 -7.25,7.25h-.4a6.3,6.3 0 0 1 -5.05,-2.55L4.1,15.55a1.55,1.55 0 0 1 2.35,-2.02L8,15.25",
    ],
  },
  happy: {
    strokes: [RING, "M8.5,14.5C9.4,15.7 10.6,16.25 12,16.25S14.6,15.7 15.5,14.5"],
    fills: [dot(9, 9.75), dot(15, 9.75)],
  },
  help: {
    strokes: [RING, "M9.5,9.4a2.55,2.55 0 1 1 3.6,2.3c-.65,.32 -1.1,.9 -1.1,1.6v.45"],
    fills: [dot(12, 16.75)],
  },
  hourglass: {
    strokes: [
      "M5.75,2.75H18.25",
      "M5.75,21.25H18.25",
      "M7,3V5.4a3.5,3.5 0 0 0 1.1,2.55L12,12L15.9,7.95A3.5,3.5 0 0 0 17,5.4V3",
      "M7,21V18.6a3.5,3.5 0 0 1 1.1,-2.55L12,12L15.9,16.05A3.5,3.5 0 0 1 17,18.6V21",
    ],
  },
  image: {
    strokes: [
      box(3, 3, 18, 18, 4.5),
      circle(8.5, 8.5, 1.75),
      "M3.25,17L7.55,12.95a1.6,1.6 0 0 1 2.1,-.05L15.5,18",
      "M13,15.75l2.45,-2.15a1.6,1.6 0 0 1 2.1,0L20.75,16.4",
    ],
  },
  info: { strokes: [RING, "M12,11.25V16.25"], fills: [dot(12, 7.9)] },
  "info-fill": {
    strokes: [],
    fills: [circle(12, 12, 10) + capsule([12, 11], [12, 16.5], HOLE) + dot(12, 7.75, 1.1)],
  },
  key: {
    strokes: [circle(8.25, 15.75, 5), "M11.75,12.25L20.25,3.75", "M17.5,6.5L19.75,8.75", "M15.1,8.9L16.85,10.65"],
    fills: [dot(7.75, 16.25, 1)],
  },
  keyboard: {
    strokes: [box(2.5, 5, 19, 14, 4), "M8.75,15.25H15.25"],
    fills: [dot(6.75, 9.25), dot(10.25, 9.25), dot(13.75, 9.25), dot(17.25, 9.25), dot(6.75, 12.25), dot(10.25, 12.25), dot(13.75, 12.25), dot(17.25, 12.25)],
  },
  keypad: {
    strokes: [],
    fills: [5.5, 12, 18.5].flatMap((y) => [5.5, 12, 18.5].map((x) => dot(x, y, 1.6))),
  },
  layers: { strokes: STACK },
  library: {
    strokes: [
      box(3, 3.25, 4.75, 17.5, 1.75),
      box(7.75, 6.25, 4.5, 14.5, 1.75),
      "M3,7.5H7.75",
      "M7.75,10.25H12.25",
      "M13.82,6.43l2.74,-.73a1.75,1.75 0 0 1 2.14,1.24l3.06,11.42a1.75,1.75 0 0 1 -1.24,2.14l-2.74,.73a1.75,1.75 0 0 1 -2.14,-1.24L12.58,8.57A1.75,1.75 0 0 1 13.82,6.43Z",
    ],
  },
  link: {
    strokes: [
      "M9.5,14.5L14.5,9.5",
      "M11,6.75l1.6,-1.6a4.6,4.6 0 0 1 6.5,6.5L17.5,13.25",
      "M13,17.25l-1.6,1.6a4.6,4.6 0 0 1 -6.5,-6.5L6.5,10.75",
    ],
  },
  list: {
    strokes: ["M9,6.25H20.25", "M9,12H20.25", "M9,17.75H20.25"],
    fills: [dot(4.75, 6.25, 1.15), dot(4.75, 12, 1.15), dot(4.75, 17.75, 1.15)],
  },
  lock: {
    strokes: [box(4, 10, 16, 11.25, 3.5), "M7.75,10V7.5a4.25,4.25 0 0 1 8.5,0V10", "M12,14.5V16.75"],
  },
  "lock-open": {
    strokes: [box(4, 10, 16, 11.25, 3.5), "M7.75,10V7.5a4.25,4.25 0 0 1 8.2,-1.55", "M12,14.5V16.75"],
  },
  map: {
    strokes: [
      soft(
        [
          [3, 6.25],
          [9, 3.5],
          [15, 6.25],
          [21, 3.5],
          [21, 17.75],
          [15, 20.5],
          [9, 17.75],
          [3, 20.5],
        ],
        1.5,
        true,
      ),
      "M9,3.75V17.5",
      "M15,6.5V20.25",
    ],
  },
  memory: {
    strokes: [
      box(2.75, 5.5, 18.5, 11, 3),
      "M7.25,9.5V12.5",
      "M12,9.5V12.5",
      "M16.75,9.5V12.5",
      "M6.5,16.5V19",
      "M10,16.5V19",
      "M14,16.5V19",
      "M17.5,16.5V19",
    ],
  },
  menu: { strokes: ["M4.75,9H19.25", "M4.75,15H19.25"] },
  mic: { strokes: [box(8.5, 2.75, 7, 11.5, 3.5), "M5.25,11a6.75,6.75 0 0 0 13.5,0", "M12,17.75V21.25"] },
  moon: {
    strokes: ["M20.5,14.6A8.75,8.75 0 1 1 9.4,3.5A7,7 0 0 0 20.5,14.6Z"],
  },
  "more-circle": { strokes: [RING], fills: [dot(8, 12), dot(12, 12), dot(16, 12)] },
  "more-horizontal": { strokes: [], fills: [dot(5.25, 12, 1.5), dot(12, 12, 1.5), dot(18.75, 12, 1.5)] },
  "more-vertical": { strokes: [], fills: [dot(12, 5.25, 1.5), dot(12, 12, 1.5), dot(12, 18.75, 1.5)] },
  navigate: {
    strokes: [
      soft(
        [
          [4, 4],
          [20.25, 10.25],
          [12.5, 12.5],
          [10.25, 20.25],
        ],
        1.4,
        true,
      ),
    ],
  },
  "notifications-off": {
    strokes: [
      "M8.6,4.95A5.5,5.5 0 0 1 17.5,9.25V12c0,1.5 .8,2.4 1.7,3.3a1,1 0 0 1 -.7,1.7H17",
      "M14,17H5.5a1,1 0 0 1 -.7,-1.7C5.7,14.4 6.5,13.5 6.5,12V9.25",
      "M9.75,20a2.5,2.5 0 0 0 4.5,0",
      "M3.75,3.75L20.25,20.25",
    ],
  },
  "open-external": {
    strokes: [
      "M10.75,4H7.5A3.5,3.5 0 0 0 4,7.5v9A3.5,3.5 0 0 0 7.5,20h9a3.5,3.5 0 0 0 3.5,-3.5V13.25",
      soft(
        [
          [14.25, 3.75],
          [20.25, 3.75],
          [20.25, 9.75],
        ],
        1.5,
      ),
      "M19.75,4.25L11.75,12.25",
    ],
  },
  options: {
    strokes: ["M3.25,7.25H13.5", "M18.5,7.25H20.75", circle(16, 7.25, 2.5), "M3.25,16.75H5.5", "M10.5,16.75H20.75", circle(8, 16.75, 2.5)],
  },
  "paper-plane": {
    strokes: [
      soft(
        [
          [20.75, 3.25],
          [14.6, 20.4],
          [10.9, 13.1],
          [3.6, 9.4],
        ],
        1.2,
        true,
      ),
      "M10.9,13.1L16,8",
    ],
  },
  pause: { strokes: [box(5.5, 4.5, 4.5, 15, 2), box(14, 4.5, 4.5, 15, 2)] },
  "pause-circle": { strokes: [RING, "M10,9V15", "M14,9V15"] },
  paw: {
    strokes: [
      circle(5.4, 10.6, 1.85),
      circle(9.25, 6.25, 2),
      circle(14.75, 6.25, 2),
      circle(18.6, 10.6, 1.85),
      "M12,12.5c-2.5,0 -4.9,3.1 -5.45,5.6a2.4,2.4 0 0 0 3,2.8c.9,-.28 1.65,-.4 2.45,-.4s1.55,.12 2.45,.4a2.4,2.4 0 0 0 3,-2.8C16.9,15.6 14.5,12.5 12,12.5Z",
    ],
  },
  people: {
    strokes: [
      circle(9, 7.75, 3.75),
      "M2.75,20.25c.45,-3.6 3,-6 6.25,-6s5.8,2.4 6.25,6",
      "M15.5,4.4a3.75,3.75 0 0 1 0,6.7",
      "M17.75,14.5c2,.75 3.25,2.85 3.5,5.75",
    ],
  },
  person: { strokes: [circle(12, 7.5, 4.25), "M4.75,20.75c.6,-3.9 3.6,-6.5 7.25,-6.5s6.65,2.6 7.25,6.5"] },
  "person-circle": {
    strokes: [RING, circle(12, 10, 3.25), "M6.1,18.6c1.3,-2.05 3.45,-3.35 5.9,-3.35s4.6,1.3 5.9,3.35"],
  },
  "person-remove": {
    strokes: [circle(9.5, 7.5, 4), "M2.75,20.75c.55,-3.75 3.3,-6.25 6.75,-6.25s6.2,2.5 6.75,6.25", "M16.25,10.5H21.25"],
  },
  phone: { strokes: [box(6, 2.75, 12, 18.5, 3.5), "M10.5,18H13.5"] },
  "play-forward": {
    strokes: [
      soft(
        [
          [3.25, 6.5],
          [11.5, 12],
          [3.25, 17.5],
        ],
        1.8,
        true,
      ),
      soft(
        [
          [12.5, 6.5],
          [20.75, 12],
          [12.5, 17.5],
        ],
        1.8,
        true,
      ),
    ],
  },
  plugins: {
    strokes: [
      "M9,2.75V6.5",
      "M15,2.75V6.5",
      "M7.75,6.5h8.5a1.25,1.25 0 0 1 1.25,1.25V10a5.5,5.5 0 0 1 -11,0V7.75a1.25,1.25 0 0 1 1.25,-1.25Z",
      "M12,15.5V21.25",
    ],
  },
  power: { strokes: ["M12,3V11", "M7.25,5.9A8.25,8.25 0 1 0 16.75,5.9"] },
  pulse: {
    strokes: [
      soft(
        [
          [2.75, 12],
          [6.25, 12],
          [8.75, 6],
          [13, 18],
          [16, 9.75],
          [17.6, 12],
          [21.25, 12],
        ],
        0.9,
      ),
    ],
  },
  puzzle: {
    strokes: [
      "M6.25,7.25H8.4A2.5,2.5 0 1 1 12.6,7.25H14.5A2.5,2.5 0 0 1 17,9.75V11.4A2.5,2.5 0 1 1 17,15.6V18.25A2.5,2.5 0 0 1 14.5,20.75H6.25A2.5,2.5 0 0 1 3.75,18.25V9.75A2.5,2.5 0 0 1 6.25,7.25Z",
    ],
  },
  "qr-code": {
    strokes: [
      box(3, 3, 7.25, 7.25, 2.25),
      box(13.75, 3, 7.25, 7.25, 2.25),
      box(3, 13.75, 7.25, 7.25, 2.25),
      "M13.75,13.75V17",
      "M17,13.75H21",
      "M17.25,17.25V21",
      "M21,17.5V21",
      "M13.75,21H14",
    ],
    fills: [dot(6.63, 6.63, 1.1), dot(17.38, 6.63, 1.1), dot(6.63, 17.38, 1.1)],
  },
  radio: {
    strokes: [
      circle(12, 12, 2),
      "M8.45,8.45a5,5 0 0 0 0,7.1",
      "M15.55,8.45a5,5 0 0 1 0,7.1",
      "M5.6,5.6a9,9 0 0 0 0,12.8",
      "M18.4,5.6a9,9 0 0 1 0,12.8",
    ],
  },
  "radio-on": { strokes: [RING], fills: [circle(12, 12, 4.75)] },
  reader: { strokes: [box(3, 3.25, 18, 17.5, 4.25), "M7.5,8.5H16.5", "M7.5,12H16.5", "M7.5,15.5H12.5"] },
  refresh: {
    strokes: [
      "M20,12A8,8 0 1 1 17.66,6.34",
      soft(
        [
          [20.25, 3.5],
          [20.25, 8],
          [15.75, 8],
        ],
        1.3,
      ),
    ],
  },
  remove: { strokes: ["M4.75,12H19.25"] },
  "remove-circle": { strokes: [RING, "M8.25,12H15.75"] },
  reorder: { strokes: ["M4.25,6.5H19.75", "M4.25,12H19.75", "M4.25,17.5H19.75"] },
  resources: {
    strokes: [
      "M4.6,18.25A8.9,8.9 0 1 1 19.4,18.25",
      "M12.9,13.05L15.75,9.5",
      circle(12, 14.25, 1.5),
    ],
  },
  "return-forward": {
    strokes: [
      "M4.25,4.25V11a4,4 0 0 0 4,4h11.5",
      soft(
        [
          [15.5, 10.75],
          [19.75, 15],
          [15.5, 19.25],
        ],
        1,
      ),
    ],
  },
  scan: {
    strokes: [
      "M3,8.25V6.75A3.75,3.75 0 0 1 6.75,3H8.25",
      "M15.75,3h1.5A3.75,3.75 0 0 1 21,6.75v1.5",
      "M21,15.75v1.5A3.75,3.75 0 0 1 17.25,21h-1.5",
      "M8.25,21H6.75A3.75,3.75 0 0 1 3,17.25v-1.5",
      "M7,12H17",
    ],
  },
  search: { strokes: [circle(11, 11, 7.5), "M16.5,16.5L20.75,20.75"] },
  server: {
    strokes: [box(3, 3.5, 18, 7.5, 3), box(3, 13, 18, 7.5, 3), "M13.5,7.25H17", "M13.5,16.75H17"],
    fills: [dot(7.25, 7.25), dot(7.25, 16.75)],
  },
  sessions: { strokes: TERMINAL },
  settings: { strokes: [gear(), circle(12, 12, 3)] },
  share: {
    strokes: [
      "M12,3.5V14.25",
      soft(
        [
          [8.25, 7.25],
          [12, 3.5],
          [15.75, 7.25],
        ],
        1,
      ),
      "M8.5,10H7.25a3.25,3.25 0 0 0 -3.25,3.25v4.5a3.25,3.25 0 0 0 3.25,3.25h9.5A3.25,3.25 0 0 0 20,17.75v-4.5A3.25,3.25 0 0 0 16.75,10H15.5",
    ],
  },
  "shield-check": {
    strokes: [
      "M11.3,3.05a2,2 0 0 1 1.4,0l5.4,1.95A2,2 0 0 1 19.5,6.9v4.55c0,4.25 -2.85,7.75 -7.05,9.65a1.1,1.1 0 0 1 -.9,0C7.35,19.2 4.5,15.7 4.5,11.45V6.9A2,2 0 0 1 5.9,5Z",
      soft(
        [
          [8.75, 12],
          [11, 14.25],
          [15.25, 9.75],
        ],
        0.8,
      ),
    ],
  },
  skills: { strokes: STACK },
  square: { strokes: [box(3.5, 3.5, 17, 17, 4.5)] },
  stats: { strokes: [box(3.25, 12, 4.5, 8.75, 1.75), box(9.75, 3.25, 4.5, 17.5, 1.75), box(16.25, 7.75, 4.5, 13, 1.75)] },
  stop: { strokes: [box(5, 5, 14, 14, 3.75)] },
  "stop-circle": { strokes: [RING, box(9, 9, 6, 6, 1.5)] },
  sun: { strokes: [circle(12, 12, 4), ...rays(12, 12, 7, 9)] },
  "swap-horizontal": {
    strokes: [
      "M3.5,8H20",
      soft(
        [
          [16.5, 4.5],
          [20, 8],
          [16.5, 11.5],
        ],
        1,
      ),
      "M20.5,16H4",
      soft(
        [
          [7.5, 12.5],
          [4, 16],
          [7.5, 19.5],
        ],
        1,
      ),
    ],
  },
  "swap-vertical": {
    strokes: [
      "M8,3.5V20",
      soft(
        [
          [4.5, 16.5],
          [8, 20],
          [11.5, 16.5],
        ],
        1,
      ),
      "M16,20.5V4",
      soft(
        [
          [12.5, 7.5],
          [16, 4],
          [19.5, 7.5],
        ],
        1,
      ),
    ],
  },
  sync: {
    strokes: [
      "M19.6,10A7.75,7.75 0 0 0 5.4,7.75",
      soft(
        [
          [19.75, 4.5],
          [19.75, 9.75],
          [14.5, 9.75],
        ],
        1.3,
      ),
      "M4.4,14A7.75,7.75 0 0 0 18.6,16.25",
      soft(
        [
          [4.25, 19.5],
          [4.25, 14.25],
          [9.5, 14.25],
        ],
        1.3,
      ),
    ],
  },
  terminal: { strokes: TERMINAL },
  text: {
    strokes: [
      soft(
        [
          [2.75, 18.5],
          [7.5, 5.75],
          [12.25, 18.5],
        ],
        1,
      ),
      "M4.5,14H10.5",
      circle(17.5, 15.25, 3.25),
      "M20.75,11.75V18.5",
    ],
  },
  time: {
    strokes: [
      RING,
      soft(
        [
          [12, 7.5],
          [12, 12],
          [15, 14],
        ],
        0.8,
      ),
    ],
  },
  trash: {
    strokes: [
      "M3.75,6H20.25",
      "M9,6V5.25a2.25,2.25 0 0 1 2.25,-2.25h1.5A2.25,2.25 0 0 1 15,5.25V6",
      "M5.75,6l.8,12.15a3,3 0 0 0 3,2.85h4.9a3,3 0 0 0 3,-2.85L18.25,6",
      "M10,10.75V16.25",
      "M14,10.75V16.25",
    ],
  },
  undo: {
    strokes: [
      soft(
        [
          [8, 13.5],
          [3.5, 9],
          [8, 4.5],
        ],
        1,
      ),
      "M3.75,9H15a5.5,5.5 0 0 1 0,11H11",
    ],
  },
  warning: {
    strokes: ["M10.27,4.2a2,2 0 0 1 3.46,0l7.6,13.25a2,2 0 0 1 -1.73,3H4.4a2,2 0 0 1 -1.73,-3Z", "M12,9.5V13.5"],
    fills: [dot(12, 16.9)],
  },
  "warning-fill": {
    strokes: [],
    fills: [
      "M10.1,3.7a2.2,2.2 0 0 1 3.8,0l8.05,14a2.2,2.2 0 0 1 -1.9,3.3H3.95a2.2,2.2 0 0 1 -1.9,-3.3Z" +
        capsule([12, 9.25], [12, 13.5], HOLE) +
        dot(12, 16.9, 1.1),
    ],
  },
} as const satisfies Record<string, MewlaGlyph>;
