/**
 * The seal cat's geometry, copied from the landing (site/sealcat.js and
 * site/seal-icon.svg), which stays the single source: sealCatGeometry.test.ts
 * fails if either copy drifts. Everything here is pure data and math so the
 * app can draw the same cat with react-native-svg; the landing's ink-paste
 * filters, physics and play are not ported.
 */

export const SEAL_RED = "#C8372B";
export const SEAL_RED_INK = "#A92B21";
export const SEAL_PAPER = "#FBF6EC";

// ---- the curled cat: feet at the origin, head left, tail wrapped in front --
export const CURL = {
  tail: "M 41 -12 C 46 4 24 7.5 4 5.5 C -6 4.6 -14 4 -20 4.6 C -28 5.2 -34 3 -36 -3",
  body: "M -14 2 C -26 1 -30 -12 -22 -24 C -14 -38 2 -47 18 -45 C 34 -43 44 -30 43 -16 C 42 -4 34 2 22 2 Z",
  earL: "M -35.5 -30 C -37.5 -38 -38 -46 -36 -53 C -30 -49 -25.5 -45 -22.5 -40.5 Z",
  earR: "M -14.5 -41 C -10 -46 -5.5 -50 0 -52 C 1 -46 0.5 -39 -2 -33 Z",
  head: [-19, -25, 18.5, 16, -6],
  paw: "M -31 -10 C -34 -4 -29 0 -21 0 C -15 0 -12 -3 -14 -7 C -17 -10 -26 -11 -31 -10 Z",
  nose: "M -21.4 -21.6 L -17.8 -21.6 L -19.6 -19.5 Z",
  lines: [
    "M -2.2 -38 C 3 -30 3 -18 -3 -10",
    "M 36 -5 C 33 -16 22 -20 13 -13",
    "M 38.4 -8 C 40 2 24 3.2 6 1.2",
    "M -30 -26.5 Q -27 -23.6 -24 -26.5", "M -15.5 -27 Q -12.5 -24.1 -9.5 -27",
  ],
  fine: [
    "M -19.6 -19.6 Q -21.3 -17.2 -23.2 -18.4", "M -19.6 -19.6 Q -17.9 -17.2 -16 -18.4",
    "M -33.6 -36 C -34 -41 -34 -45 -33.2 -48", "M -10.4 -42 C -7.4 -45 -4.8 -47 -2.4 -48.2",
    "M -32.2 -20.4 L -36.2 -21.2", "M -32.2 -18.2 L -36 -17.4", "M -7 -21.2 L -3 -22.2", "M -7 -19 L -3.2 -18.4",
    "M -30 -9.4 C -24 -11.2 -18 -10.2 -14.6 -7.6",
    "M -25 -3.8 L -24.6 -0.8", "M -20.6 -3.6 L -20.2 -0.6",
  ],
  stripes: [
    "M 9 -43.4 C 11.2 -40 11.8 -37 11.2 -33.8", "M 18.8 -44 C 20.6 -40.6 20.8 -37.4 19.8 -34.4", "M 28.6 -41.4 C 29.8 -38.4 29.8 -35.6 28.8 -33",
  ],
} as const;

/** One open eye over the shut one, for a cat that is half awake. */
export const CURL_PEEK = { cx: -12.5, cy: -26, rx: 3.8, ry: 2.6, pupilRx: 1.7, pupilRy: 2.1 };

// ---- the seal (100 × 100): seal-icon.svg's clean block -----------------------
export const SEAL = {
  block: "M 4 5.2 Q 4 4 5.2 4 L 94.8 4 Q 96 4 96 5.2 L 96 94.8 Q 96 96 94.8 96 L 5.2 96 Q 4 96 4 94.8 Z",
  moon: { cx: 79, cy: 19, r: 7.4 },
  moonBite: { cx: 82.6, cy: 16.4, r: 6.4 },
  chips: [
    "M 4 31 L 6.2 32.4 L 5.4 35 L 4 35.6 Z",
    "M 63 96 L 64.6 93.6 L 67.4 94.4 L 68 96 Z",
    "M 96 70 L 94.4 71.2 L 94.8 73.4 L 96 73.8 Z",
  ],
} as const;

/** Where the curled cat sleeps inside the seal: translate x, y and scale. */
export const SEAL_AT = [44.6, 77, 0.9] as const;
export const CAT_IN_SEAL = `translate(${SEAL_AT[0]} ${SEAL_AT[1]}) scale(${SEAL_AT[2]})`;

/**
 * The landing thickens the carving for a small seal so it still reads at
 * icon size, and drops whiskers, toes and the mouth below a pixel.
 */
export function sealCarving(sizePx: number): { bold: number; detail: boolean } {
  const px = sizePx / 100;
  return { bold: clamp(0.6 / px, 1, 2.6), detail: sizePx >= 64 };
}

// ---- the standing cat: one rig, many poses ---------------------------------
// Feet on y = 0, facing right. Leg angles are radians from straight down,
// positive forward. Tail angles are degrees: 0 forward, 90 down, 180 back.
export const STAND_VIEWBOX = "-70 -100 140 110";

export const BASE = {
  bx: 0, by: -22, brx: 24, bry: 14, brot: 0,
  hx: 23, hy: -36, hrot: 0,
  fn: -0.05, ff: 0.15, rn: 0.12, rf: -0.1, lenF: 14, lenR: 13, ground: 1,
  fold: 0, tuck: 0, paw: 0,
  ta: 215, tc: 40, tl: 34, ears: 0,
  eyes: "open", mouth: "none",
};

export const POSES = {
  stand: {},
  sit: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 9, hy: -52, fn: 0.02, ff: 0.1, fold: 1, ta: 176, tc: 75, tl: 33 },
  loaf: { by: -14, brx: 25, bry: 13, hx: 22, hy: -26, fold: 1, tuck: 1, ta: 178, tc: 55, tl: 33, eyes: "happy" },
  crouch: { by: -15, bry: 12, hx: 24, hy: -26, ears: 0.3, ta: 190, tc: 20 },
  fly: { by: -24, brot: -6, fn: 1.1, ff: 0.9, rn: -1.0, rf: -1.2, ground: 0, ta: 200, tc: 30 },
  reach: { by: -24, fn: 0.5, ff: 0.4, rn: -0.5, rf: -0.6, ground: 0, lenF: 15, lenR: 14 },
  stretch: { bx: -2, by: -20, brot: 16, hx: 26, hy: -16, fn: 1.05, ff: 0.95, lenF: 16, ta: 260, tc: 30, eyes: "shut" },
  dangle: { bx: 0, by: -36, brx: 15, bry: 20, brot: -84, hx: 2, hy: -66, hrot: 0, fn: 0.7, ff: 0.5, rn: 0.15, rf: -0.15, lenF: 13, lenR: 15, ground: 0, ta: 100, tc: 10, tl: 30, ears: 0.6, eyes: "wide" },
  paw: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 9, hy: -52, ff: 0.1, fold: 1, paw: 1, ta: 176, tc: 75, tl: 33 },
  groom: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 11, hy: -47, hrot: 18, ff: 0.1, fold: 1, paw: 0.75, ta: 176, tc: 75, tl: 33, eyes: "shut" },
  yawn: { bx: -3, by: -25, brx: 20, bry: 15, brot: -48, hx: 8, hy: -54, hrot: -14, fold: 1, ta: 176, tc: 75, tl: 33, eyes: "shut", mouth: "open" },
  alert: { by: -26, bry: 14, hx: 24, hy: -42, lenF: 17, lenR: 16, ears: 0, ta: 268, tc: 6, tl: 36, eyes: "wide" },
  stalk: { by: -13, brx: 25, bry: 12, hx: 27, hy: -19, fn: 0.25, rn: -0.2, ears: 0.15, ta: 176, tc: 12, tl: 36, eyes: "wide" },
  wiggle: { bx: -1, by: -14, brx: 24, bry: 12, brot: 8, hx: 27, hy: -18, fn: 0.35, ff: 0.25, rn: -0.45, rf: -0.55, ears: 0.1, ta: 192, tc: 40, tl: 36, eyes: "wide" },
  pin: { by: -13, bry: 12, brot: 10, hx: 31, hy: -14, hrot: 12, fn: 1, ff: 0.9, lenF: 16, rn: -0.3, rf: -0.4, ta: 200, tc: 30, eyes: "wide" },
  rear: { bx: -4, by: -32, brx: 18, bry: 14, brot: -78, hx: 3, hy: -66, hrot: -12, fn: 2.5, ff: 2.8, lenF: 15, fold: 1, ta: 165, tc: 60, eyes: "wide" },
  leap: { bx: -2, by: -34, brx: 17, bry: 14, brot: -70, hx: 4, hy: -64, hrot: -10, fn: 2.6, ff: 2.9, lenF: 14, rn: 0.3, rf: 0.1, lenR: 15, ground: 0, ta: 120, tc: 50, eyes: "wide" },
  sniff: { by: -18, hx: 28, hy: -17, hrot: 22, ta: 230, tc: 30 },
};

export type PoseName = keyof typeof POSES;
export type Pose = typeof BASE;
export type EyeShape = "open" | "wide" | "shut" | "happy";

export function pose(name: PoseName, extra?: Partial<Pose>): Pose {
  return { ...BASE, ...POSES[name], ...extra };
}

/** Fixed pieces of the standing rig, in torso or head coordinates. */
export const RIG = {
  marks: ["M -7 -13.4 q 1.6 4 0.6 8", "M 1 -14 q 1.6 4 0.6 8", "M 9 -13.2 q 1.3 3.6 0.3 7"],
  haunchLine: "M -15 9 C -18 1 -12 -6 -3 -4",
  frontToes: "M -1 -4.4 L -1 -1.8 M 2 -4.4 L 2 -1.8",
  earL: "M -13 -5 C -14.4 -12 -13.4 -18 -10.4 -20.4 C -6.4 -18.4 -3.4 -14.6 -2 -11 Z",
  earR: "M 2 -11.6 C 4 -15.6 7.4 -18.6 11.4 -20.4 C 13.8 -17 14 -11 12.8 -5 Z",
  earLines: ["M -9.6 -9.5 L -9.8 -15", "M 9.2 -9.8 L 9.4 -15.2"],
  whiskers: ["M 13 3 L 18.6 2.2", "M 13 5.6 L 18.4 6.6", "M -6 3.4 L -10.6 2.6"],
  eyesShut: ["M -5 -1 Q -2 1.6 1 -1", "M 6 -1 Q 9 1.6 12 -1"],
  eyesHappy: ["M -5 0 Q -2 -2.8 1 0", "M 6 0 Q 9 -2.8 12 0"],
  nose: "M 2.6 3.2 L 5.8 3.2 L 4.2 5.2 Z",
  smile: ["M 4.2 5.2 Q 2.8 7.4 1.2 6.4", "M 4.2 5.2 Q 5.6 7.4 7.2 6.4"],
} as const;

export interface RigLine {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

/** One frame of the standing cat, ready to draw. */
export interface StandingCatFrame {
  legsFar: [RigLine | null, RigLine | null];
  legsNear: [RigLine | null, RigLine | null];
  tail: string;
  torso: string;
  body: { rx: number; ry: number };
  haunch: { rx: number; ry: number };
  haunchLineOpacity: number;
  rearPaws: { x: number; opacity: number };
  frontPaws: { x: number; opacity: number };
  head: string;
  earL: string;
  earR: string;
  earLinesOpacity: number;
  eyes: EyeShape;
  mouthOpen: boolean;
}

/**
 * The landing's render() for one moment, as data. step is the walk swing
 * (sin of the walk phase times the stride), swish the tail's sway in degrees
 * and breath the torso's vertical scale.
 */
export function standingCatFrame(
  P: Pose,
  { step = 0, swish = 0, breath = 1 }: { step?: number; swish?: number; breath?: number } = {},
): StandingCatFrame {
  const a = (P.brot * Math.PI) / 180;
  const c = Math.cos(a);
  const s = Math.sin(a);
  const rigPoint = (lx: number, ly: number): [number, number] => [
    P.bx + lx * c - ly * s,
    P.by + lx * s + ly * c,
  ];
  const leg = ([hx, hy]: [number, number], ang: number, len: number, show: number): RigLine | null => {
    if (show < 0.12) return null;
    let L = len;
    if (P.ground > 0) {
      const down = Math.cos(ang);
      const reach = down > 0.2 ? clamp((-hy - 1) / down, 4, 28) : len;
      L = lerp(len, reach, P.ground);
    }
    L *= show;
    return { x1: hx, y1: hy, x2: hx + Math.sin(ang) * L, y2: hy + Math.cos(ang) * L };
  };
  const showF = 1 - P.tuck;
  const showR = 1 - Math.max(P.fold, P.tuck);
  const hp = rigPoint(-6, 10);

  const A = rigPoint(-21, -3);
  const sw = swish * (1 - P.fold * 0.6);
  const th = P.ta + sw * 0.4;
  const cu = P.tc + sw;
  const p1 = dirv(th, P.tl * 0.42);
  const p2 = dirv(th + cu * 0.45, P.tl * 0.78);
  const p3 = dirv(th + cu, P.tl * 0.36);
  const tail = `M ${f(A[0])} ${f(A[1])} C ${f(A[0] + p1[0])} ${f(A[1] + p1[1])} ${f(A[0] + p2[0])} ${f(A[1] + p2[1])} ${f(A[0] + p2[0] + p3[0])} ${f(A[1] + p2[1] + p3[1])}`;
  const flat = P.ears * 38;

  return {
    legsFar: [
      leg(rigPoint(16, 3), P.ff - step, P.lenF, showF),
      leg(rigPoint(-17, 3), P.rf + step, P.lenR, showR),
    ],
    legsNear: [
      leg(rigPoint(13, 5), lerp(P.fn + step, 2.25, P.paw), lerp(P.lenF, 11, P.paw), showF),
      leg(rigPoint(-14, 5), P.rn - step, P.lenR, showR),
    ],
    tail,
    torso: `translate(${f(P.bx)} ${f(P.by)}) rotate(${P.brot.toFixed(1)}) scale(1 ${breath.toFixed(3)})`,
    body: { rx: P.brx, ry: P.bry },
    haunch: { rx: 13 * P.fold, ry: 12 * P.fold },
    haunchLineOpacity: 1 - P.fold,
    rearPaws: { x: hp[0] + 9, opacity: P.fold },
    frontPaws: { x: P.bx + 19, opacity: P.tuck },
    head: `translate(${f(P.hx)} ${f(P.hy)}) rotate(${P.hrot.toFixed(1)})`,
    earL: `rotate(${(-flat).toFixed(1)} -8 -9)`,
    earR: `rotate(${flat.toFixed(1)} 7 -9)`,
    earLinesOpacity: 1 - P.ears,
    eyes: P.eyes as EyeShape,
    mouthOpen: P.mouth === "open",
  };
}

function dirv(deg: number, length: number): [number, number] {
  const radians = (deg * Math.PI) / 180;
  return [Math.cos(radians) * length, Math.sin(radians) * length];
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function lerp(a: number, b: number, t: number): number {
  return a + (b - a) * t;
}

function f(value: number): string {
  return value.toFixed(2);
}
