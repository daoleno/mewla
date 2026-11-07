// Pure helpers behind the file tree runtime: status mapping, row decoration
// text, and long-press timing, shared by the WebView and unit tests.

export type TreeGitStatus = "added" | "deleted" | "modified" | "renamed" | "untracked";

/** Git review statuses the daemon reports, mapped to the ones the tree draws. */
export function treeGitStatus(status: string): TreeGitStatus {
  switch (status) {
    case "added":
    case "copied":
      return "added";
    case "deleted":
      return "deleted";
    case "renamed":
      return "renamed";
    case "untracked":
      return "untracked";
    default:
      // changed, conflict and modified all read as an edit to the file.
      return "modified";
  }
}

export interface TreeDecoration {
  additions: number;
  deletions: number;
  binary?: boolean;
}

export interface DecorationPart {
  text: string;
  color?: string;
}

/** The `+N −M` lane; a file with no counted lines shows nothing. */
export function decorationParts(
  decoration: TreeDecoration | undefined,
  colors: { added: string; deleted: string; muted: string },
): DecorationPart[] | null {
  if (!decoration) return null;
  if (decoration.binary) return [{ text: "Binary", color: colors.muted }];
  if (!decoration.additions && !decoration.deletions) return null;
  const parts: DecorationPart[] = [];
  if (decoration.additions) parts.push({ text: `+${decoration.additions}`, color: colors.added });
  if (decoration.deletions) {
    parts.push({ text: `${parts.length ? " " : ""}−${decoration.deletions}`, color: colors.deleted });
  }
  return parts;
}

export const LONG_PRESS_MS = 420;
/** A finger that travels farther than this is scrolling, not pressing. */
export const LONG_PRESS_SLOP_PX = 8;

export function movedPastSlop(startX: number, startY: number, x: number, y: number): boolean {
  return Math.hypot(x - startX, y - startY) > LONG_PRESS_SLOP_PX;
}
