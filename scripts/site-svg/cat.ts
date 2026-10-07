// Prints the standing seal cat as static SVG for the README drawings, from the
// app's geometry (app/components/mewla/sealCatGeometry.ts) and the drawing in
// SealCat.tsx. Output: JSON {pose: "<g>…</g>"} in the crop -58 -80 110 84.
// Run: bun scripts/site-svg/cat.ts
import {
  RIG,
  SEAL_PAPER,
  SEAL_RED,
  SEAL_RED_INK,
  pose,
  standingCatFrame,
  type RigLine,
  type StandingCatFrame,
} from "../../app/components/mewla/sealCatGeometry";

const line = (l: RigLine | null, color: string, width: number) =>
  l
    ? `<line x1="${l.x1.toFixed(2)}" y1="${l.y1.toFixed(2)}" x2="${l.x2.toFixed(2)}" y2="${l.y2.toFixed(2)}" stroke="${color}" stroke-width="${width}" stroke-linecap="round"/>`
    : "";
const carve = (d: string, width: number) =>
  `<path d="${d}" fill="none" stroke="${SEAL_PAPER}" stroke-width="${width}" stroke-linecap="round"/>`;

function eyes(shape: StandingCatFrame["eyes"]): string {
  if (shape === "wide") {
    return [-2, 9]
      .map(
        (cx) =>
          `<ellipse cx="${cx}" cy="-1.2" rx="2.7" ry="3.1" fill="${SEAL_PAPER}"/><circle cx="${cx + 0.5}" cy="-0.8" r="1.2" fill="${SEAL_RED_INK}"/>`,
      )
      .join("");
  }
  if (shape === "shut" || shape === "happy") {
    const paths = shape === "shut" ? RIG.eyesShut : RIG.eyesHappy;
    return paths
      .map((d) => `<path d="${d}" fill="none" stroke="${SEAL_PAPER}" stroke-width="1.8" stroke-linecap="round"/>`)
      .join("");
  }
  return `<g fill="${SEAL_PAPER}"><ellipse cx="-2" cy="-1" rx="1.9" ry="2.4"/><ellipse cx="9" cy="-1" rx="1.9" ry="2.4"/></g>`;
}

function draw(frame: StandingCatFrame): string {
  const out: string[] = [];
  out.push(line(frame.legsFar[0], SEAL_RED_INK, 8), line(frame.legsFar[1], SEAL_RED_INK, 8));
  out.push(`<path d="${frame.tail}" fill="none" stroke="${SEAL_RED}" stroke-width="8" stroke-linecap="round"/>`);
  out.push(
    `<path d="${frame.tail}" fill="none" stroke="${SEAL_PAPER}" stroke-width="8.4" stroke-dasharray="1.3 6.2" stroke-dashoffset="-9"/>`,
  );
  out.push(`<g transform="${frame.torso}">`);
  if (frame.haunch.rx > 0) {
    out.push(`<ellipse cx="-9" cy="2" rx="${frame.haunch.rx}" ry="${frame.haunch.ry}" fill="${SEAL_RED}"/>`);
  }
  out.push(`<ellipse cx="0" cy="0" rx="${frame.body.rx}" ry="${frame.body.ry}" fill="${SEAL_RED}"/>`);
  out.push(...RIG.marks.map((d) => carve(d, 1.9)));
  if (frame.haunchLineOpacity > 0) out.push(`<g opacity="${frame.haunchLineOpacity}">${carve(RIG.haunchLine, 1.6)}</g>`);
  out.push("</g>");
  if (frame.rearPaws.opacity > 0) {
    out.push(
      `<g transform="translate(${frame.rearPaws.x.toFixed(2)} 0)" opacity="${frame.rearPaws.opacity}"><ellipse cx="0" cy="-3" rx="6.4" ry="3.4" fill="${SEAL_RED}"/></g>`,
    );
  }
  out.push(line(frame.legsNear[1], SEAL_RED, 8.5), line(frame.legsNear[0], SEAL_RED, 8.5));
  if (frame.frontPaws.opacity > 0) {
    out.push(
      `<g transform="translate(${frame.frontPaws.x.toFixed(2)} 0)" opacity="${frame.frontPaws.opacity}"><ellipse cx="0" cy="-3" rx="6" ry="3.4" fill="${SEAL_RED}"/>${carve(RIG.frontToes, 0.8)}</g>`,
    );
  }
  out.push(`<g transform="${frame.head}">`);
  out.push(
    `<path d="${RIG.earL}" transform="${frame.earL}" fill="${SEAL_RED}" stroke="${SEAL_RED}" stroke-width="3" stroke-linejoin="round"/>`,
    `<path d="${RIG.earR}" transform="${frame.earR}" fill="${SEAL_RED}" stroke="${SEAL_RED}" stroke-width="3" stroke-linejoin="round"/>`,
    `<ellipse cx="0" cy="0" rx="16.5" ry="14.5" fill="${SEAL_RED}"/>`,
  );
  if (frame.earLinesOpacity > 0) {
    out.push(`<g opacity="${frame.earLinesOpacity}">${RIG.earLines.map((d) => carve(d, 1.1)).join("")}</g>`);
  }
  out.push(...RIG.whiskers.map((d) => carve(d, 0.9)), eyes(frame.eyes));
  out.push(
    `<path d="${RIG.nose}" fill="${SEAL_PAPER}" stroke="${SEAL_PAPER}" stroke-width="0.7" stroke-linejoin="round"/>`,
  );
  out.push(
    frame.mouthOpen
      ? `<ellipse cx="4.2" cy="8" rx="2.4" ry="3" fill="${SEAL_PAPER}"/>`
      : RIG.smile.map((d) => carve(d, 0.9)).join(""),
  );
  out.push("</g>");
  return `<g>${out.join("")}</g>`;
}

process.stdout.write(
  JSON.stringify({
    // Work needs you: alert, ears up (perched on the Needs-you slip).
    alert: draw(standingCatFrame(pose("alert"))),
    // Brain's turn running: mid-stride.
    walk: draw(standingCatFrame(pose("stand"), { step: 0.4, swish: 4 })),
    // Delegated Work on Workers: sitting.
    sit: draw(standingCatFrame(pose("sit"))),
  }),
);
