import { brandMark, type BrandMark, type RowLeading, type StateTone } from "../more/colour";
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

/** Services whose id is not their product's name: Google Workspace is Google's G, Google Cloud its cloud. */
const SERVICE_MARKS: Record<string, BrandMark> = { gws: "google", gcp: "googlecloud" };

/** Services with no mark of their own: a glyph for what they are. */
const GLYPHS: Record<string, "server" | "code"> = { mcp: "server", openapi: "code" };

export type ServiceGlyph = "server" | "code" | "puzzle";

/**
 * A service's leading icon: its mark in its real colours when the client has it (GitHub, Notion, Linear, Slack,
 * Google Workspace, Google Cloud…), its own one-colour mark drawn in ink (`service`: AWS, Hyperliquid, Onlora),
 * else its glyph on the neutral tile.
 */
export type ServiceLead = RowLeading<ServiceGlyph> | { kind: "service"; id: string };

export function serviceLead(id: string): ServiceLead {
  const mark = SERVICE_MARKS[id] ?? brandMark(id);
  if (mark) return { kind: "mark", mark };
  if (hasServiceMark(id)) return { kind: "service", id };
  return { kind: "tile", glyph: GLYPHS[id] ?? "puzzle" };
}
