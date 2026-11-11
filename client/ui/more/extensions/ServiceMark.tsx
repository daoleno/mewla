import { Canvas, Path2D, View } from "nsh";
import { SERVICE_MARKS } from "../../../core/extensions/serviceMarkPaths";
import { color } from "../kit/colors";
import { Icon, type IconName } from "../kit/Icon";

/** Glyphs for the services with no brand mark of their own here. */
const GLYPHS: Record<string, IconName> = {
  tailscale: "git-network",
  mcp: "server",
  openapi: "code",
};

const paths = new Map<string, Path2D>();

/**
 * A service's mark in the text colour: connection state is the status mark's job,
 * not the logo's. `dim` when every account of the service is off, a second cue
 * beside the Off it says.
 */
export function ServiceMark(props: { id: string; size?: number; dim?: boolean }) {
  const size = () => props.size ?? 22;
  const mark = SERVICE_MARKS[props.id];
  if (!mark) {
    return (
      <View style={{ opacity: props.dim ? 0.4 : 1 }}>
        <Icon name={GLYPHS[props.id] ?? "puzzle"} size={size()} />
      </View>
    );
  }
  const [x, y, w, h] = (mark.viewBox ?? "0 0 24 24").split(/\s+/).map(Number);
  return (
    <View style={{ opacity: props.dim ? 0.4 : 1 }}>
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
    </View>
  );
}
