/**
 * Colour behind the first level (soft-final SPEC §10.4), as decisions the pages make from what the daemon
 * reports: which leading mark a row gets, and which state tone a status or a meter shows. The values live in
 * ui/theme.json; this file only names them.
 */

/** A row tile's hue: the hue of the row's group (§10.4 "Tokens"). */
export type TileHue = "blue" | "violet" | "teal" | "pink" | "graphite";

/**
 * A state as colour shows it (§10.4 "State"): `ok` healthy, on, connected; `warn` degraded or needs setting up,
 * nothing blocked; `error` broken and stopping work (red: it is your move); `off` absence, soft and no dot.
 */
export type StateTone = "ok" | "warn" | "error" | "off";

/** The named products whose real mark the client draws (simple-icons, CC0). */
export const BRAND_MARKS = ["telegram", "github", "notion", "linear", "nodejs", "go", "docker", "python", "postgresql", "sentry", "vercel", "cloudflare", "figma"] as const;
export type BrandMark = (typeof BRAND_MARKS)[number];

/** The names a product goes by (a service id, a process name, a label), folded to its mark. */
const ALIASES: Record<string, BrandMark> = {
  telegram: "telegram",
  github: "github",
  gh: "github",
  notion: "notion",
  linear: "linear",
  node: "nodejs",
  nodejs: "nodejs",
  npm: "nodejs",
  npx: "nodejs",
  go: "go",
  golang: "go",
  gopls: "go",
  docker: "docker",
  dockerd: "docker",
  python: "python",
  python3: "python",
  postgres: "postgresql",
  postgresql: "postgresql",
  psql: "postgresql",
  sentry: "sentry",
  vercel: "vercel",
  cloudflare: "cloudflare",
  cloudflared: "cloudflare",
  wrangler: "cloudflare",
  figma: "figma",
};

/** The mark a named product is drawn with, or null when the client has none (`Node.js`, `node`, `GitHub` all match). */
export function brandMark(name: string | undefined | null): BrandMark | null {
  if (!name) return null;
  const key = name.trim().toLowerCase().replace(/\.exe$/, "").replace(/[\s._-]+/g, "");
  return ALIASES[key] ?? null;
}

/** A row's leading icon: a named product's mark, or the generic glyph on its group's tile. Never a bare grey outline. */
export type RowLeading<Glyph extends string = string> =
  | { kind: "mark"; mark: BrandMark }
  | { kind: "tile"; hue: TileHue; glyph: Glyph };

/** The leading icon of a row for `name`: its mark when the client has one, else `fallback` (the group's tile). */
export function rowLeading<Glyph extends string>(name: string | undefined | null, fallback: { hue: TileHue; glyph: Glyph }): RowLeading<Glyph> {
  const mark = brandMark(name);
  return mark ? { kind: "mark", mark } : { kind: "tile", hue: fallback.hue, glyph: fallback.glyph };
}

/** Where a meter's fill turns (shares, 0..1). Where a metric has no threshold of the daemon's: 80 % and 95 % (§10.4 "Data"). */
export interface MeterThresholds {
  warn: number;
  critical: number;
}

export const DEFAULT_METER_THRESHOLDS: MeterThresholds = { warn: 0.8, critical: 0.95 };

/** A meter's fill: its own colour under the warning threshold, `warn` over it, `error` over the critical one. */
export function meterTone(share: number, thresholds: MeterThresholds = DEFAULT_METER_THRESHOLDS): "fill" | "warn" | "error" {
  if (share >= thresholds.critical) return "error";
  if (share >= thresholds.warn) return "warn";
  return "fill";
}
