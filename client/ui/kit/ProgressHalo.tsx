import { Canvas, View } from "nsh";
import { progressMarkLabel, type JudgedOutcomeState, type ProgressMark } from "../../core/work/judgedProgress";
import { color, type ColorName } from "./colors";

/**
 * A Work's judged progress (SPEC §11 Progress): a ring around the row's tile, drawn from twelve o'clock. No number
 * is written anywhere; the ring's label speaks it. The colour says where the Work stands, the one colour on the
 * first level besides red: `meter` moving, `warning` sent back, `ok` and closed in the last stretch (never "100").
 */
const TONE = { moving: "meter", back: "warning", last: "ok" } as const satisfies Record<string, ColorName>;

function toneOf(mark: ProgressMark): ColorName {
  return mark.kind === "last" ? TONE.last : TONE[mark.tone];
}

/** The ring alone, `size` square: 32 around a row's 28 pt tile, 56 around Work detail's logo. */
export function ProgressHalo(props: { mark: () => ProgressMark; size?: number; testID?: string }) {
  const size = props.size ?? 32;
  const stroke = size >= 48 ? 4 : 2.5;
  return (
    <View testID={props.testID ?? "progress-mark"} role="progressbar" accessibilityLabel={progressMarkLabel(props.mark())}
      style={{ width: size, height: size }}>
      <Canvas
        style={{ width: size, height: size }}
        draw={(cx, s) => {
          const mark = props.mark();
          const w = s.width;
          const h = s.height;
          const r = (Math.min(w, h) - stroke) / 2;
          cx.lineWidth = stroke;
          cx.strokeStyle = color("track");
          cx.beginPath();
          cx.arc(w / 2, h / 2, r, 0, Math.PI * 2);
          cx.stroke();
          // The last stretch closes the ring but for a small gap at its head: never a full "100".
          const sweep = mark.kind === "last" ? Math.PI * 2 - (stroke * 1.6) / r : (Math.PI * 2 * mark.percent) / 100;
          if (sweep <= 0) return;
          cx.strokeStyle = color(toneOf(mark));
          cx.lineCap = "round";
          cx.beginPath();
          cx.arc(w / 2, h / 2, r, -Math.PI / 2, -Math.PI / 2 + sweep);
          cx.stroke();
        }}
      />
    </View>
  );
}

/**
 * An outcome's mark on Work detail (SPEC §11 Progress): done is a filled `meter` disc with a check, under way a
 * third of the ring, to do the track alone.
 */
export function OutcomeMark(props: { state: () => JudgedOutcomeState; size?: number }) {
  const size = props.size ?? 20;
  return (
    <Canvas
      style={{ width: size, height: size }}
      draw={(cx, s) => {
        const state = props.state();
        const w = s.width;
        const h = s.height;
        const stroke = 2;
        const r = (Math.min(w, h) - stroke) / 2;
        if (state === "done") {
          cx.fillStyle = color("meter");
          cx.beginPath();
          cx.arc(w / 2, h / 2, Math.min(w, h) / 2, 0, Math.PI * 2);
          cx.fill();
          cx.strokeStyle = color("surface");
          cx.lineWidth = 2;
          cx.lineCap = "round";
          cx.lineJoin = "round";
          cx.beginPath();
          cx.moveTo(w * 0.3, h * 0.52);
          cx.lineTo(w * 0.44, h * 0.66);
          cx.lineTo(w * 0.7, h * 0.36);
          cx.stroke();
          return;
        }
        cx.lineWidth = stroke;
        cx.strokeStyle = color("track");
        cx.beginPath();
        cx.arc(w / 2, h / 2, r, 0, Math.PI * 2);
        cx.stroke();
        if (state === "doing") {
          cx.strokeStyle = color("meter");
          cx.lineCap = "round";
          cx.beginPath();
          cx.arc(w / 2, h / 2, r, -Math.PI / 2, -Math.PI / 2 + Math.PI * 2 * 0.35);
          cx.stroke();
        }
      }}
    />
  );
}
