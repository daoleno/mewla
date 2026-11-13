import type { RowLeading, StateTone } from "../more/colour";
import type { FixRow } from "./needsFixing";

/**
 * The computer page's colour (soft-final SPEC §10.4): a Needs fixing row is its thing's glyph on the neutral tile
 * with its words in `warn`, or red
 * where the computer cannot work for this phone until it is fixed (pair again, a required update). Chrome's own
 * multicolour mark is not in the client yet, so the extension row keeps the generic tile.
 */
export type FixGlyph = "link" | "cloud-offline" | "brain" | "puzzle" | "desktop" | "cloud-download" | "alert-circle";

export function fixRowLead(row: Pick<FixRow, "key">): RowLeading<FixGlyph> {
  if (row.key === "pair") return { kind: "tile", glyph: "link" };
  if (row.key === "connection") return { kind: "tile", glyph: "cloud-offline" };
  if (row.key === "host") return { kind: "tile", glyph: "brain" };
  if (row.key === "browser") return { kind: "tile", glyph: "puzzle" };
  if (row.key === "screen") return { kind: "tile", glyph: "desktop" };
  if (row.key.startsWith("update")) return { kind: "tile", glyph: "cloud-download" };
  return { kind: "tile", glyph: "alert-circle" };
}

/** A Needs fixing row's words: red when blocking, `warn` when something waits for a fix, soft while it is under way. */
export function fixRowTone(row: Pick<FixRow, "blocking" | "button" | "key">): StateTone {
  if (row.blocking) return "error";
  if (row.key === "update-optional") return "off";
  return row.button ? "warn" : "off";
}

/** The header's connection: connected is ok, this computer offline is red (work stops), the rest soft. */
export function connectionTone(state: "connected" | "connecting" | "waiting" | "idle"): StateTone {
  if (state === "connected") return "ok";
  if (state === "waiting") return "error";
  return "off";
}
