// Find and hunk navigation over parsed patch metadata. Pure, so the same code
// runs inside the diff WebView and under unit tests.

export interface NavContext {
  lines: number;
  additionLineIndex: number;
  deletionLineIndex: number;
}
export interface NavChange {
  additions: number;
  additionLineIndex: number;
  deletions: number;
  deletionLineIndex: number;
}
export interface NavHunk {
  additionStart: number;
  deletionStart: number;
  hunkContent: ({ type: "context" } & NavContext | { type: "change" } & NavChange)[];
}
export interface NavSection {
  id: string;
  hunks: NavHunk[];
  additionLines: string[];
  deletionLines: string[];
}
export interface LineRef {
  id: string;
  side: "additions" | "deletions";
  line: number;
}

/** Every line of a section in reading order: deletions before additions inside a change. */
export function sectionLines(section: NavSection): (LineRef & { text: string })[] {
  const lines: (LineRef & { text: string })[] = [];
  for (const hunk of section.hunks) {
    let oldLine = hunk.deletionStart;
    let newLine = hunk.additionStart;
    for (const block of hunk.hunkContent) {
      if (block.type === "context") {
        for (let offset = 0; offset < block.lines; offset++) {
          // Context shows once, on the new side, like the reader does.
          lines.push({
            id: section.id,
            side: "additions",
            line: newLine++,
            text: section.additionLines[block.additionLineIndex + offset] ?? "",
          });
          oldLine++;
        }
        continue;
      }
      for (let offset = 0; offset < block.deletions; offset++) {
        lines.push({
          id: section.id,
          side: "deletions",
          line: oldLine++,
          text: section.deletionLines[block.deletionLineIndex + offset] ?? "",
        });
      }
      for (let offset = 0; offset < block.additions; offset++) {
        lines.push({
          id: section.id,
          side: "additions",
          line: newLine++,
          text: section.additionLines[block.additionLineIndex + offset] ?? "",
        });
      }
    }
  }
  return lines;
}

/** Lines containing the query, case-insensitively, across every section. */
export function findMatches(sections: readonly NavSection[], query: string): LineRef[] {
  const needle = query.toLowerCase();
  if (!needle) return [];
  const matches: LineRef[] = [];
  for (const section of sections) {
    for (const line of sectionLines(section)) {
      if (line.text.toLowerCase().includes(needle)) {
        matches.push({ id: line.id, side: line.side, line: line.line });
      }
    }
  }
  return matches;
}

/** First line of every hunk, in reading order. */
export function hunkStarts(sections: readonly NavSection[]): LineRef[] {
  const starts: LineRef[] = [];
  for (const section of sections) {
    for (const hunk of section.hunks) {
      const first = hunk.hunkContent[0];
      const deletionsFirst = first?.type === "change" && first.additions === 0 && first.deletions > 0;
      starts.push({
        id: section.id,
        side: deletionsFirst ? "deletions" : "additions",
        line: deletionsFirst ? hunk.deletionStart : hunk.additionStart,
      });
    }
  }
  return starts;
}

/** Wraps around: past the last match comes the first, before the first the last. */
export function stepIndex(current: number, count: number, direction: "next" | "previous"): number {
  if (count <= 0) return -1;
  if (current < 0) return direction === "next" ? 0 : count - 1;
  return direction === "next" ? (current + 1) % count : (current - 1 + count) % count;
}

/**
 * The hunk to land on from the one at `current`: hunk navigation clamps at the
 * ends so the control never silently jumps across the whole file.
 */
export function stepHunk(current: number, count: number, direction: "next" | "previous"): number {
  if (count <= 0) return -1;
  if (current < 0) return direction === "next" ? 0 : count - 1;
  return Math.min(count - 1, Math.max(0, current + (direction === "next" ? 1 : -1)));
}
