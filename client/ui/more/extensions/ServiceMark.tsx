import { Canvas, Path2D } from "nsh";
import { serviceLead } from "../../../core/extensions/colour";
import { SERVICE_MARKS } from "../../../core/extensions/serviceMarkPaths";
import { color } from "../../kit/colors";
import type { Lead } from "../../kit/RowTile";

const paths = new Map<string, Path2D>();

/**
 * A service's own one-colour mark (AWS, Hyperliquid, Onlora), drawn in ink so it flips in dark. The marks the
 * kit has in their real colours, and the generic tiles, come from `serviceRowLead`.
 */
export function ServiceMark(props: { id: string; size?: number }) {
  const size = () => props.size ?? 28;
  const mark = SERVICE_MARKS[props.id];
  if (!mark) return null;
  const [x, y, w, h] = (mark.viewBox ?? "0 0 24 24").split(/\s+/).map(Number);
  return (
    <Canvas
      style={{ width: size(), height: size() }}
      accessibilityElementsHidden
      draw={(cx) => {
        let path = paths.get(props.id);
        if (!path) {
          path = new Path2D(mark.d);
          paths.set(props.id, path);
        }
        const scale = size() / Math.max(w, h);
        cx.save();
        cx.scale(scale, scale);
        cx.translate(-x, -y);
        cx.fillStyle = color("ink");
        cx.fill(path, mark.evenOdd ? "evenodd" : "nonzero");
        cx.restore();
      }}
    />
  );
}

/** A service's lead (SPEC §10.4): its real mark, its own mark in ink, or its glyph on the neutral tile. `size` is the mark's. */
export function serviceRowLead(id: string, size = 28): Lead {
  const lead = serviceLead(id);
  return lead.kind === "service" ? { kind: "logo", node: <ServiceMark id={id} size={size} /> } : lead;
}
