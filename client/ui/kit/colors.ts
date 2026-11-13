import { token, type Tokens } from "nsh";

/**
 * The colours the pages draw with outside classes (canvas strokes, status marks, icons), by name. Every one is
 * a role of ui/theme.json (soft-final SPEC §10.1 and §10.4), read through `token()` so the values live in the
 * theme once and follow the appearance shown. Red (`seal`) means waiting for you and nothing else; on the first
 * level a state's tone names its role and reads in ink or soft. `ok`, `warning`, `meter`, the neutral tile and
 * the series are for the pages behind the first level only (§10.4), with one exception: a Work's judged progress
 * ring (`ProgressHalo`, §9 and §11 Progress) uses `meter`, `warning` and `ok` on the first level too.
 */
const ROLES = {
  ink: "foreground",
  soft: "muted-foreground",
  faint: "faint",
  hair: "border",
  track: "track",
  canvas: "background",
  surface: "card",
  seal: "seal",
  sealInk: "seal-ink",
  /** On a seal fill (Approve's check): white in both schemes. */
  onSeal: "seal-foreground",
  add: "add",
  del: "del",
  ready: "muted-foreground",
  running: "foreground",
  failed: "foreground",
  blocked: "faint",
  /** Healthy, on, connected (§10.4). */
  ok: "ok",
  /** Degraded or needs setting up, nothing blocked (§10.4); amber behind the first level. */
  warning: "warning",
  /** A meter's fill under its threshold. */
  meter: "meter",
  /** The one neutral tile under a generic row's ink glyph (§10.4). */
  tile: "tile",
  /** The agent series (§10.4): Claude, Codex, OpenCode, Cursor (and the fourth agent), Other. */
  seriesClaude: "chart-1",
  seriesCodex: "chart-2",
  seriesOpencode: "chart-3",
  seriesCursor: "chart-4",
  seriesOther: "chart-5",
} as const satisfies Record<string, keyof Tokens>;

/** On the full-screen remote's and the picture viewer's black (SPEC §15): white and its soft, in both schemes. */
const ON_BLACK = { onBlack: "#FFFFFF", onBlackSoft: "#A2A5AC" } as const;

export type ColorName = keyof typeof ROLES | keyof typeof ON_BLACK;

/** The colour now, following the appearance shown (read it inside JSX or a canvas `draw`). */
export function color(name: ColorName): string {
  if (name === "onBlack" || name === "onBlackSoft") return ON_BLACK[name];
  return String(token(ROLES[name]));
}

/** A colour (`#rrggbb` or `#rrggbbaa`) at `alpha` (0..1), for washes under a chart line. */
export function withAlpha(hex: string, alpha: number): string {
  const a = Math.round(Math.max(0, Math.min(1, alpha)) * 255).toString(16).padStart(2, "0");
  return `${hex.slice(0, 7)}${a}`;
}
