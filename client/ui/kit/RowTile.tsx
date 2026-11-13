import type { ImageAsset, JSX } from "nsh";
import { Canvas, Image, Path2D, Text, View, colorScheme } from "nsh";
import type { BrandMark, RowLeading, StateTone } from "../../core/more/colour";
import { color, type ColorName } from "./colors";
import { Icon, type IconName } from "./Icon";
import { MARK_PATHS } from "./marks";

/**
 * A row's leading icon behind the first level (soft-final SPEC §10.4 "Row tiles"), one of three things and never
 * a bare grey outline: a named product's real mark or an agent's logo (28 in a 30 slot, no tile), a thing's own
 * art, or a generic setting's glyph on the one neutral tile (30 × 30, radius 9, the glyph 18 in ink on the `tile`
 * wash). The tile carries no colour: colour stays for marks, state, meters and series.
 * The first level and its family (Home, the tray, a Session, Work detail, All Sessions, their sheets and menus)
 * never use it. The kit imports no pictures (a test harness with its own nsh.json could not bundle them), so a
 * thing's art comes in as its asset, and an agent's logo as the page's `AgentIcon` (`logo`), sized here.
 */
export type Lead = RowLeading<IconName> | { kind: "art"; src: ImageAsset } | { kind: "logo"; node: JSX.Element };

/** The slot a lead takes in a row; the row's divider starts after it (§10.4: inset 60). */
export const LEAD_SLOT = 30;

/** A generic glyph on the neutral tile. */
export function tile(glyph: IconName): Lead {
  return { kind: "tile", glyph };
}

export function RowTile(props: { lead: Lead; testID?: string }) {
  const lead = () => props.lead;
  return (
    <View testID={props.testID} class="items-center justify-center" style={{ width: LEAD_SLOT, height: LEAD_SLOT }}>
      {(() => {
        const l = lead();
        if (l.kind === "tile") {
          return (
            <View class="items-center justify-center" style={{ width: LEAD_SLOT, height: LEAD_SLOT, borderRadius: 9, backgroundColor: color("tile") }}>
              <Icon name={l.glyph} size={18} tone="ink" />
            </View>
          );
        }
        if (l.kind === "mark") return <MarkView mark={l.mark} size={28} />;
        if (l.kind === "logo") return l.node;
        return <Image src={l.src} style={{ width: 28, height: 28 }} />;
      })()}
    </View>
  );
}

/**
 * A detail page's own top block (SPEC §15, §10.4): the same lead at 64 — a mark or a logo at 40 on the surface, a
 * thing's art at 40, or the glyph at 30 in ink on the neutral tile, radius 20.
 */
export function LeadHero(props: { lead: Lead; testID?: string }) {
  const l = () => props.lead;
  return (
    <View testID={props.testID} class="items-center justify-center overflow-hidden" style={{ width: 64, height: 64, borderRadius: 20, backgroundColor: color(l().kind === "tile" ? "tile" : "surface") }}>
      {(() => {
        const x = l();
        if (x.kind === "tile") return <Icon name={x.glyph} size={30} tone="ink" />;
        if (x.kind === "mark") return <MarkView mark={x.mark} size={40} />;
        if (x.kind === "logo") return x.node;
        return <Image src={x.src} style={{ width: 40, height: 40 }} />;
      })()}
    </View>
  );
}

const compiled = new Map<string, Path2D>();
function path(mark: BrandMark): Path2D {
  let p = compiled.get(mark);
  if (!p) {
    p = new Path2D(MARK_PATHS[mark].d);
    compiled.set(mark, p);
  }
  return p;
}

/** A named product's mark at `size`, in its real colours (ink for black marks). */
export function MarkView(props: { mark: BrandMark; size?: number }) {
  const size = () => props.size ?? 28;
  const mark = props.mark;
  return (
    <Canvas
      style={{ width: size(), height: size() }}
      accessibilityElementsHidden
      draw={(cx) => {
        const fill = MARK_PATHS[mark].fill;
        cx.save();
        cx.scale(size() / 24, size() / 24);
        // A knockout mark keeps its cut-out white in dark: a white plate under the plane (§10.4).
        if (mark === "telegram") {
          cx.fillStyle = color("onBlack");
          cx.beginPath();
          cx.arc(12, 12, 9.6, 0, Math.PI * 2);
          cx.fill();
        }
        cx.fillStyle = fill === "ink" ? color("ink") : typeof fill === "string" ? fill : colorScheme() === "dark" ? color("ink") : fill.brand;
        cx.fill(path(mark), "evenodd");
        cx.restore();
      }}
    />
  );
}

const STATE_COLOR: Record<Exclude<StateTone, "off">, { dot: ColorName; text: ColorName }> = {
  ok: { dot: "ok", text: "soft" },
  warn: { dot: "warning", text: "warning" },
  error: { dot: "seal", text: "sealInk" },
};

/** The colour of a state's words: soft for ok and off, `warning` for warn, `seal-ink` for error. */
export function stateTextColor(tone: StateTone): ColorName {
  return tone === "off" ? "soft" : STATE_COLOR[tone].text;
}

/** A meter's fill colour: its own (`meter`, or an agent's series), `warning` over the warning threshold, red over the critical one. */
export function meterColor(tone: "fill" | "warn" | "error", fill: ColorName = "meter"): ColorName {
  return tone === "error" ? "seal" : tone === "warn" ? "warning" : fill;
}

/**
 * A state in its fixed place (§10.4 "State"): an 8 pt dot and the words. `ok` keeps the words soft, `warn` and
 * `error` colour them, `off` is soft words and no dot. The colour is never the only sign: the words say it.
 * `detail` follows in soft ("· updated 13:41").
 */
export function StateLine(props: { tone: StateTone; label: string; detail?: string; testID?: string; strong?: boolean }) {
  const look = () => (props.tone === "off" ? null : STATE_COLOR[props.tone]);
  return (
    <View testID={props.testID} class="shrink flex-row items-center gap-1.5" accessible accessibilityLabel={props.detail ? `${props.label} ${props.detail}` : props.label}>
      {look() ? <View testID={props.testID ? `${props.testID}-dot` : undefined} class="size-2 rounded-full" style={{ backgroundColor: color(look()!.dot) }} /> : null}
      <Text testID={props.testID ? `${props.testID}-words` : undefined} class={`shrink text-sub line-clamp-1 ${props.strong ? "font-semibold" : ""}`} style={{ color: color(stateTextColor(props.tone)) }}>{props.label}</Text>
      {props.detail ? <Text class="shrink text-sub text-muted-foreground line-clamp-1">{props.detail}</Text> : null}
    </View>
  );
}
