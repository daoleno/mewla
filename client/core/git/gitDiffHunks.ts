/**
 * One file's patch split into hunks, so the sheet can stage, unstage or
 * discard one hunk and quote lines to the agent. The reader draws the same
 * patch text; both number its lines from the `@@` headers, so a line the
 * reader reports maps to the same hunk here.
 */
export type GitPatchSide = "additions" | "deletions";

export interface GitPatchLine {
  kind: " " | "+" | "-";
  oldLine: number | null;
  newLine: number | null;
  text: string;
}

export interface GitPatchHunk {
  index: number;
  /** The `@@ -a,b +c,d @@ context` line. */
  header: string;
  oldStart: number;
  oldCount: number;
  newStart: number;
  newCount: number;
  lines: GitPatchLine[];
  /** From the `@@` line on, as `git apply` takes it. */
  text: string;
}

export interface GitParsedPatch {
  hunks: GitPatchHunk[];
  /** Hunks exist only for text patches; a binary or mode-only change has none. */
  binary: boolean;
}

const HUNK_HEADER = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/;

export function parseGitPatch(patch: string): GitParsedPatch {
  const rows = patch.split("\n");
  const hunks: GitPatchHunk[] = [];
  let binary = false;
  let current: GitPatchHunk | null = null;
  let oldLine = 0;
  let newLine = 0;
  let textRows: string[] = [];
  const close = () => {
    if (current) current.text = `${textRows.join("\n")}\n`;
  };
  for (const row of rows) {
    const match = HUNK_HEADER.exec(row);
    if (match) {
      close();
      oldLine = Number(match[1]);
      newLine = Number(match[3]);
      current = {
        index: hunks.length,
        header: row,
        oldStart: oldLine,
        oldCount: match[2] === undefined ? 1 : Number(match[2]),
        newStart: newLine,
        newCount: match[4] === undefined ? 1 : Number(match[4]),
        lines: [],
        text: "",
      };
      textRows = [row];
      hunks.push(current);
      continue;
    }
    if (!current) {
      if (row.startsWith("Binary files ") || row === "GIT binary patch") binary = true;
      continue;
    }
    const mark = row[0];
    if (mark === "+") {
      current.lines.push({ kind: "+", oldLine: null, newLine: newLine++, text: row.slice(1) });
    } else if (mark === "-") {
      current.lines.push({ kind: "-", oldLine: oldLine++, newLine: null, text: row.slice(1) });
    } else if (mark === " ") {
      current.lines.push({ kind: " ", oldLine: oldLine++, newLine: newLine++, text: row.slice(1) });
    } else if (mark === "\\") {
      // "\ No newline at end of file" belongs to the line before it.
    } else {
      continue;
    }
    textRows.push(row);
  }
  close();
  return { hunks, binary };
}

/** The hunk that holds a line the reader selected, or null for a line outside every hunk. */
export function gitHunkAtLine(parsed: GitParsedPatch, side: GitPatchSide, line: number): GitPatchHunk | null {
  for (const hunk of parsed.hunks) {
    const found = hunk.lines.some((entry) => (side === "additions" ? entry.newLine : entry.oldLine) === line);
    if (found) return hunk;
  }
  return null;
}

/** The range that highlights a whole hunk in the reader. */
export function gitHunkSelection(hunk: GitPatchHunk): { side: GitPatchSide; start: number; end: number } | null {
  const added = hunk.lines.filter((line) => line.newLine !== null);
  if (added.length) return { side: "additions", start: added[0].newLine!, end: added[added.length - 1].newLine! };
  const removed = hunk.lines.filter((line) => line.oldLine !== null);
  if (removed.length) return { side: "deletions", start: removed[0].oldLine!, end: removed[removed.length - 1].oldLine! };
  return null;
}

/** The lines of a selected range, in the order the hunk shows them. */
export function gitSelectedLines(
  hunk: GitPatchHunk,
  side: GitPatchSide,
  start: number,
  end: number,
): GitPatchLine[] {
  const low = Math.min(start, end);
  const high = Math.max(start, end);
  return hunk.lines.filter((line) => {
    const number = side === "additions" ? line.newLine : line.oldLine;
    return number !== null && number >= low && number <= high;
  });
}

export function gitHunkChangeCounts(hunk: GitPatchHunk): { additions: number; deletions: number } {
  let additions = 0;
  let deletions = 0;
  for (const line of hunk.lines) {
    if (line.kind === "+") additions += 1;
    else if (line.kind === "-") deletions += 1;
  }
  return { additions, deletions };
}

/**
 * A commit's patch cut into one patch per file, in the order git printed
 * them. `files` is the daemon's list for the same commit: when it has as many
 * entries as the patch has file blocks they pair up by position, which also
 * holds for paths git quotes; otherwise each block is matched by its `b/` path.
 */
export function splitGitPatchByFile(patch: string, files: readonly { path: string }[]): Map<string, string> {
  const blocks = patch.split(/^(?=diff --git )/m).filter((block) => block.startsWith("diff --git "));
  const out = new Map<string, string>();
  if (blocks.length === files.length) {
    files.forEach((file, index) => out.set(file.path, blocks[index].replace(/\n+$/, "")));
    return out;
  }
  for (const block of blocks) {
    const header = block.slice(0, block.indexOf("\n") === -1 ? undefined : block.indexOf("\n"));
    const at = header.lastIndexOf(" b/");
    const path = at === -1 ? "" : header.slice(at + 3);
    if (path) out.set(path, block.replace(/\n+$/, ""));
  }
  return out;
}
