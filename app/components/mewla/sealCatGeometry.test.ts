import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  BASE,
  CURL,
  CURL_PEEK,
  POSES,
  RIG,
  SEAL,
  SEAL_AT,
  SEAL_PAPER,
  SEAL_RED,
  SEAL_RED_INK,
  pose,
  sealCarving,
  standingCatFrame,
} from "./sealCatGeometry";

// The landing owns the cat; the app copy must not drift from it.
const site = join(import.meta.dir, "../../../site");
const sealcat = readFileSync(join(site, "sealcat.js"), "utf8");
const sealIcon = readFileSync(join(site, "seal-icon.svg"), "utf8");

function literal(name: string): unknown {
  const start = sealcat.indexOf(`const ${name} = {`);
  expect(start).toBeGreaterThanOrEqual(0);
  const end = sealcat.indexOf("\n};", start);
  const source = sealcat.slice(start + `const ${name} = `.length, end + 2);
  return new Function(`return (${source});`)();
}

describe("seal cat geometry matches the landing", () => {
  test("curled cat paths, poses and base rig are copied verbatim", () => {
    expect(JSON.parse(JSON.stringify(CURL))).toEqual(literal("CURL"));
    expect(BASE).toEqual(literal("BASE") as typeof BASE);
    expect(POSES).toEqual(literal("POSES") as typeof POSES);
  });

  test("standing rig pieces, colours and the seal placement come from sealcat.js", () => {
    const pieces = [
      ...RIG.marks, RIG.haunchLine, RIG.frontToes, RIG.earL, RIG.earR,
      ...RIG.earLines, ...RIG.whiskers, ...RIG.eyesShut, ...RIG.eyesHappy,
      RIG.nose, ...RIG.smile,
    ];
    for (const d of pieces) expect(sealcat).toContain(`'${d}'`);
    expect(sealcat).toContain(`RED = '${SEAL_RED.toLowerCase()}'`);
    expect(sealcat).toContain(`RED2 = '${SEAL_RED_INK.toLowerCase()}'`);
    expect(sealcat).toContain(`PAPER = '${SEAL_PAPER.toLowerCase()}'`);
    expect(sealcat).toContain(`const SEAL_AT = [${SEAL_AT.join(", ")}];`);
    expect(sealcat).toContain(`cx: ${CURL_PEEK.cx}, cy: ${CURL_PEEK.cy}, rx: ${CURL_PEEK.rx}, ry: ${CURL_PEEK.ry}`);
  });

  test("the seal block, moon and chips are seal-icon.svg's", () => {
    expect(sealIcon).toContain(`d="${SEAL.block}"`);
    expect(sealIcon).toContain(`cx="${SEAL.moon.cx}" cy="${SEAL.moon.cy}" r="${SEAL.moon.r}"`);
    expect(sealIcon).toContain(`cx="${SEAL.moonBite.cx}" cy="${SEAL.moonBite.cy}" r="${SEAL.moonBite.r}"`);
    for (const d of SEAL.chips) expect(sealIcon).toContain(`d="${d}"`);
  });
});

describe("standing cat frames", () => {
  test("a standing cat keeps all four legs on the ground", () => {
    const frame = standingCatFrame(pose("stand"));
    for (const leg of [...frame.legsFar, ...frame.legsNear]) {
      expect(leg).not.toBeNull();
      expect(Math.abs(leg!.y2)).toBeLessThan(1.5);
    }
  });

  test("a sitting cat folds its hind legs into the haunch", () => {
    const frame = standingCatFrame(pose("sit"));
    expect(frame.legsFar[1]).toBeNull();
    expect(frame.legsNear[1]).toBeNull();
    expect(frame.haunch.rx).toBe(13);
    expect(frame.rearPaws.opacity).toBe(1);
  });

  test("a walk step swings near and far legs in opposition", () => {
    const left = standingCatFrame(pose("stand"), { step: 0.5 });
    const right = standingCatFrame(pose("stand"), { step: -0.5 });
    expect(Math.sign(left.legsNear[0]!.x2 - left.legsNear[0]!.x1)).toBe(1);
    expect(Math.sign(right.legsNear[0]!.x2 - right.legsNear[0]!.x1)).toBe(-1);
  });

  test("small seals carve bolder and drop fine detail, as on the landing", () => {
    expect(sealCarving(120)).toEqual({ bold: 1, detail: true });
    const icon = sealCarving(28);
    expect(icon.detail).toBe(false);
    expect(icon.bold).toBeGreaterThan(2);
  });
});
