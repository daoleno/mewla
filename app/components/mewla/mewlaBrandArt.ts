/**
 * The Mewla brand art: app icons, the Android adaptive layers, the splash
 * seal, the favicon and the notification glyph, all drawn from the seal cat's
 * geometry (site/seal-icon.svg via sealCatGeometry.ts). Pure strings so
 * scripts/render-mewla-brand.ts can write the committed SVG sources and PNGs,
 * and mewlaBrandArt.test.ts can fail when a committed file drifts.
 */
import { BRAND_COLORS } from "../../theme/primitives";
import { CAT_IN_SEAL, CURL, SEAL, sealCarving } from "./sealCatGeometry";

const RED = BRAND_COLORS.vermilion;
const PAPER = BRAND_COLORS.sealPaper;
/** The warm ink behind the seal in dark mode, as on the landing. */
const INK = BRAND_COLORS.environment;

type Carving = { bold: number; detail: boolean };

const svg = (viewBox: string, body: string) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}">\n${body}\n</svg>\n`;

const n = (value: number) => Number(value.toFixed(3)).toString();

/** The curled cat's solid shapes; `fill` paints them, `tail` strokes the tail. */
function catShapes(fill: string): string {
  const [cx, cy, rx, ry, rot] = CURL.head;
  return [
    `<path d="${CURL.tail}" fill="none" stroke="${fill}" stroke-width="9" stroke-linecap="round"/>`,
    ...[CURL.body, CURL.earL, CURL.earR].map((d) => `<path d="${d}" fill="${fill}"/>`),
    `<ellipse cx="${cx}" cy="${cy}" rx="${rx}" ry="${ry}" transform="rotate(${rot} ${cx} ${cy})" fill="${fill}"/>`,
    `<path d="${CURL.paw}" fill="${fill}"/>`,
  ].join("\n");
}

/** The knife lines carved into the cat, with the landing's small-size carving. */
function catCarving(line: string, { bold, detail }: Carving): string {
  const stroke = (d: string, width: number) =>
    `<path d="${d}" fill="none" stroke="${line}" stroke-width="${n(width)}" stroke-linecap="round" stroke-linejoin="round"/>`;
  return [
    ...CURL.lines.map((d) => stroke(d, 1.55 * bold)),
    `<path d="${CURL.nose}" fill="${line}" stroke="${line}" stroke-width="0.8" stroke-linejoin="round"/>`,
    ...(detail ? (bold > 1 ? CURL.fine.slice(2, 4) : CURL.fine).map((d) => stroke(d, 0.9 * bold)) : []),
    ...(detail ? CURL.stripes.map((d) => stroke(d, 2 * Math.sqrt(bold))) : []),
  ].join("\n");
}

function moon(paper: string, block: string): string {
  return [
    `<circle cx="${SEAL.moon.cx}" cy="${SEAL.moon.cy}" r="${SEAL.moon.r}" fill="${paper}"/>`,
    `<circle cx="${SEAL.moonBite.cx}" cy="${SEAL.moonBite.cy}" r="${SEAL.moonBite.r}" fill="${block}"/>`,
  ].join("\n");
}

/** The paper cat and moon, carved in the block colour: the seal's contents. */
function sealContents(block: string, paper: string, carving: Carving): string {
  return [
    moon(paper, block),
    `<g transform="${CAT_IN_SEAL}">\n${catShapes(paper)}\n${catCarving(block, carving)}\n</g>`,
  ].join("\n");
}

/** The block with its edge chips cut through, so the page shows in them. */
function sealBlock(fill: string, chips = true): string {
  return `<path d="${[SEAL.block, ...(chips ? SEAL.chips : [])].join(" ")}" fill="${fill}" fill-rule="evenodd"/>`;
}

/**
 * One colour on transparent: the cat and moon cut out of the block, the knife
 * lines and the moon's bite left standing. Android and iOS read only the
 * shape of these (notification, tinted icon).
 */
function knockoutSeal(fill: string, carving: Carving, id: string, chips = true): string {
  return [
    `<mask id="${id}" maskUnits="userSpaceOnUse" x="0" y="0" width="100" height="100">`,
    `<rect width="100" height="100" fill="#000"/>`,
    sealBlock("#fff", chips),
    sealContents("#fff", "#000", carving),
    `</mask>`,
    `<rect width="100" height="100" fill="${fill}" mask="url(#${id})"/>`,
  ].join("\n");
}

/** The clean seal (seal-icon.svg) carved for a rendering `sizePx` wide. */
export function sealArt(sizePx: number): string {
  return svg("0 0 100 100", [sealBlock(RED), sealContents(RED, PAPER, sealCarving(sizePx))].join("\n"));
}

/**
 * The seal's contents (cat and moon) fill a circle at centre 46.3, 47.5 with
 * radius 48.1 in seal units, measured from the render. `fit` moves that
 * circle to (x, y) with radius r, so a layer can place them by eye-safe area.
 */
const CONTENTS = { cx: 46.3, cy: 47.5, r: 48.1 } as const;
function fit(x: number, y: number, r: number): string {
  const k = r / CONTENTS.r;
  return `translate(${n(x - CONTENTS.cx * k)} ${n(y - CONTENTS.cy * k)}) scale(${n(k)})`;
}

/**
 * iOS light icon, 1024 opaque: the seal is the whole icon. iOS rounds the
 * corners, so the block runs full bleed and the cat and moon sit inside 80%
 * of it, clear of the corner curve.
 */
export function iosIconArt(): string {
  return svg(
    "0 0 100 100",
    [`<rect width="100" height="100" fill="${RED}"/>`, `<g transform="${fit(50, 50, 40)}">\n${sealContents(RED, PAPER, sealCarving(1024))}\n</g>`].join("\n"),
  );
}

/** Where the stamped seal sits on a dark or tinted icon: 72% wide, centred. */
const STAMP = "translate(14 14) scale(0.72)";

/** iOS dark icon: the seal stamped on transparent; iOS supplies the dark ground. */
export function iosIconDarkArt(): string {
  return svg("0 0 100 100", `<g transform="${STAMP}">\n${sealBlock(RED)}\n${sealContents(RED, PAPER, sealCarving(1024))}\n</g>`);
}

/** iOS tinted icon: greyscale on black; iOS tints it by luminance. */
export function iosIconTintedArt(): string {
  return svg(
    "0 0 100 100",
    [`<rect width="100" height="100" fill="#000"/>`, `<g transform="${STAMP}">\n${knockoutSeal("#fff", sealCarving(1024), "tint")}\n</g>`].join("\n"),
  );
}

/**
 * Android adaptive foreground (108 dp canvas): the paper cat and moon over
 * the red background layer, fitted into the 66 dp safe circle with a 1.5 dp
 * margin, so circle and squircle masks never clip the moon or the ears.
 */
const ADAPTIVE = fit(54, 54, 31.5);

export function adaptiveForegroundArt(): string {
  return svg("0 0 108 108", `<g transform="${ADAPTIVE}">\n${sealContents(RED, PAPER, sealCarving(432))}\n</g>`);
}

/** Android themed (monochrome) icon: the cat and crescent, knife lines cut through. */
export function adaptiveMonochromeArt(): string {
  return svg(
    "0 0 108 108",
    [
      `<mask id="mono" maskUnits="userSpaceOnUse" x="0" y="0" width="108" height="108">`,
      `<rect width="108" height="108" fill="#000"/>`,
      `<g transform="${ADAPTIVE}">\n${sealContents("#000", "#fff", sealCarving(432))}\n</g>`,
      `</mask>`,
      `<rect width="108" height="108" fill="#fff" mask="url(#mono)"/>`,
    ].join("\n"),
  );
}

/**
 * Android notification glyph (24 dp): the seal in white, cat cut out. The
 * chips would be specks at this size, and the landing's 24 px carving makes
 * the eyes heavy once inverted, so it is carved as for 36 px.
 */
export function notificationArt(): string {
  return svg("0 0 100 100", knockoutSeal("#fff", sealCarving(36), "note", false));
}

export const MEWLA_BRAND_COLORS = { red: RED, paper: PAPER, ink: INK } as const;

/**
 * Every committed brand file: its SVG source, the PNG size rendered from it,
 * and whether the PNG must carry no alpha (App Store icons are rejected with one).
 */
export const MEWLA_BRAND_ASSETS = [
  { name: "mewla-icon", art: iosIconArt, size: 1024, opaque: true },
  { name: "mewla-icon-dark", art: iosIconDarkArt, size: 1024 },
  { name: "mewla-icon-tinted", art: iosIconTintedArt, size: 1024, opaque: true },
  { name: "mewla-adaptive-foreground", art: adaptiveForegroundArt, size: 432 },
  { name: "mewla-adaptive-monochrome", art: adaptiveMonochromeArt, size: 432 },
  { name: "mewla-splash", art: () => sealArt(128), size: 512 },
  { name: "mewla-favicon", art: () => sealArt(48), size: 48 },
  { name: "mewla-notification", art: notificationArt, size: 96 },
] as const;
