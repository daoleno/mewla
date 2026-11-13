import type { GitDiffStatusTone } from "./gitDiffPresentation";

/**
 * A file's status tile (soft-final SPEC §10.4 Git review): it keeps its mark and takes its colour. Added in `add`
 * on `add` at 13 %, deleted in `del` on `del` at 11 %, modified in the meter blue on it at 12 %; renamed (a move,
 * nothing changed in it) is ink on the neutral tile. A conflict reads as deleted's red-brown (it loses work if left), an untracked file as added (it is new);
 * a binary file stays faint on the canvas.
 */
export type StatusTileLook =
  | { glyph: "add" | "del" | "meter"; wash: { of: "add" | "del" | "meter"; alpha: number } }
  | { glyph: "ink"; wash: "tile" }
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
      return { glyph: "meter", wash: { of: "meter", alpha: 0.12 } };
    case "renamed":
      return { glyph: "ink", wash: "tile" };
    default:
      return { glyph: "faint", wash: "canvas" };
  }
}
