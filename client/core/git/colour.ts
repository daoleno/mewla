import type { GitDiffStatusTone } from "./gitDiffPresentation";

/**
 * A file's status tile (soft-final SPEC §10.4 Git review): it keeps its mark and takes its colour. Added in `add`
 * on `add` at 13 %, deleted in `del` on `del` at 11 %, modified in the blue tile's glyph on its wash, renamed in the
 * violet's. A conflict reads as deleted's red-brown (it loses work if left), an untracked file as added (it is new);
 * a binary file stays faint on the canvas.
 */
export type StatusTileLook =
  | { glyph: "add" | "del"; wash: { of: "add" | "del"; alpha: number } }
  | { glyph: "tileBlue"; wash: "tileBlueWash" }
  | { glyph: "tileViolet"; wash: "tileVioletWash" }
  | { glyph: "faint"; wash: "canvas" };

export function statusTileLook(tone: GitDiffStatusTone): StatusTileLook {
  switch (tone) {
    case "added":
    case "untracked":
      return { glyph: "add", wash: { of: "add", alpha: 0.13 } };
    case "deleted":
    case "conflict":
      return { glyph: "del", wash: { of: "del", alpha: 0.11 } };
    case "modified":
      return { glyph: "tileBlue", wash: "tileBlueWash" };
    case "renamed":
      return { glyph: "tileViolet", wash: "tileVioletWash" };
    default:
      return { glyph: "faint", wash: "canvas" };
  }
}
