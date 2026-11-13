import type { BrandMark, RowLeading } from "../more/colour";
import { brainWorkspaceEntryIconName } from "./workspace";

/** Files whose name says the product they belong to (SPEC §10.4 Files: "a file with a known mark may use it"). */
const FILE_MARKS: Record<string, BrandMark> = {
  "package.json": "nodejs",
  "go.mod": "go",
  "go.sum": "go",
  dockerfile: "docker",
  "docker-compose.yml": "docker",
  "docker-compose.yaml": "docker",
  "compose.yml": "docker",
  "compose.yaml": "docker",
  "pyproject.toml": "python",
  "requirements.txt": "python",
};

/**
 * A Files or Workspace row's leading icon (SPEC §10.4): a folder on the teal tile, a file on the graphite tile with
 * its kind's glyph, or the product's mark for a file whose name names it (package.json, go.mod, Dockerfile).
 */
export function fileLead(kind: string, path: string): RowLeading<"folder" | "document-text" | "document" | "play"> {
  if (kind === "directory") return { kind: "tile", hue: "teal", glyph: "folder" };
  const name = (path.split("/").pop() ?? "").toLowerCase();
  const mark = FILE_MARKS[name];
  if (mark) return { kind: "mark", mark };
  const glyph = brainWorkspaceEntryIconName(kind, path);
  return { kind: "tile", hue: "graphite", glyph: glyph === "folder" ? "document" : glyph };
}
