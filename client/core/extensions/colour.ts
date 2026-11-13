import { brandMark, type RowLeading, type StateTone } from "../more/colour";
import type { ConnectionTone } from "./pluginConnectionsModel";
import { hasServiceMark } from "./serviceMarkPaths";

/**
 * Extensions' colour (soft-final SPEC §10.4): each service's real mark, and its state as ok, warn or off. A broken
 * service is `warn`, never red: Brain keeps working without it, and red stays "your move".
 */

/** A connection's tone as a state. */
export function connectionStateTone(tone: ConnectionTone): StateTone {
  if (tone === "success") return "ok";
  if (tone === "neutral") return "off";
  return "warn";
}

/** A tool call's outcome as a state: done is ok, a failure warn, anything else off. */
export function callStateTone(status: string): StateTone {
  const value = status.toLowerCase();
  if (value === "ok" || value === "success" || value === "succeeded") return "ok";
  if (value.includes("fail") || value.includes("error") || value.includes("denied")) return "warn";
  return "off";
}

/**
 * Services whose mark is several colours (Slack's, Google's): until each company's own kit is in the client they
 * get the generic tile, never a recoloured silhouette (§10.4 "Logos needed").
 */
const MULTICOLOUR: Record<string, "chat" | "apps" | "cloud"> = { slack: "chat", gws: "apps", gcp: "cloud" };

/** Services with no mark of their own: a glyph for what they are. */
const GLYPHS: Record<string, "server" | "code"> = { mcp: "server", openapi: "code" };

export type ServiceGlyph = "chat" | "apps" | "cloud" | "server" | "code" | "puzzle";

/**
 * A service's leading icon: its mark in its real colours when the client has it (GitHub, Notion, Linear,
 * Cloudflare, Tailscale), its own one-colour mark drawn in ink (`service`: AWS, Hyperliquid, Onlora), else its
 * glyph on the neutral tile.
 */
export type ServiceLead = RowLeading<ServiceGlyph> | { kind: "service"; id: string };

export function serviceLead(id: string): ServiceLead {
  if (MULTICOLOUR[id]) return { kind: "tile", glyph: MULTICOLOUR[id] };
  const mark = brandMark(id);
  if (mark) return { kind: "mark", mark };
  if (hasServiceMark(id)) return { kind: "service", id };
  return { kind: "tile", glyph: GLYPHS[id] ?? "puzzle" };
}
