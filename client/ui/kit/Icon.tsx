import { Canvas, Path2D } from "nsh";
import { color, type ColorName } from "./colors";
import { MEWLA_GLYPHS, type MewlaGlyph } from "./glyphs";

export type IconName = keyof typeof MEWLA_GLYPHS;

const paths = new Map<string, { strokes: Path2D[]; fills: Path2D[] }>();

function compiled(name: IconName) {
  let glyph = paths.get(name);
  if (!glyph) {
    const source: MewlaGlyph = MEWLA_GLYPHS[name];
    glyph = { strokes: source.strokes.map((d) => new Path2D(d)), fills: (source.fills ?? []).map((d) => new Path2D(d)) };
    paths.set(name, glyph);
  }
  return glyph;
}

/**
 * Mewla's own glyphs (24 grid, 1.5 stroke, round caps), drawn natively. Decorative:
 * the control around one carries the label.
 */
export function Icon(props: { name: IconName; size?: number; tone?: ColorName }) {
  const size = () => props.size ?? 20;
  return (
    <Canvas
      style={{ width: size(), height: size() }}
      accessibilityElementsHidden
      draw={(cx) => {
        const glyph = compiled(props.name);
        const ink = color(props.tone ?? "soft");
        cx.save();
        cx.scale(size() / 24, size() / 24);
        cx.lineWidth = 1.5;
        cx.lineCap = "round";
        cx.lineJoin = "round";
        cx.strokeStyle = ink;
        cx.fillStyle = ink;
        for (const path of glyph.strokes) cx.stroke(path);
        for (const path of glyph.fills) cx.fill(path, "evenodd");
        cx.restore();
      }}
    />
  );
}
