/**
 * The diff reader's own model, beside the reader view: find in the diff, where the
 * reader goes when its file leaves its group, the display options, and what Ask
 * the agent quotes. Pure, so the Git pages and the tests share it.
 */
import { formatGitAskAgentMessage } from "./gitAskAgent";
import { gitHunkSelection, gitSelectedLines, type GitParsedPatch, type GitPatchHunk, type GitPatchLine, type GitPatchSide } from "./gitDiffHunks";
import type { GitReviewEntry, GitReviewGroup } from "./gitReviewModel";

export interface GitFindMatch {
  side: GitPatchSide;
  /** The file's line, from 1. */
  line: number;
  /** The hunk that shows it. */
  hunk: number;
}

/**
 * Lines holding the query, case-insensitively, in reading order: a context line once
 * (on the new side, as the reader shows it), a removed line on the old side.
 */
export function findGitPatchMatches(parsed: GitParsedPatch | null, query: string): GitFindMatch[] {
  const needle = query.trim().toLowerCase();
  if (!needle || !parsed) return [];
  const matches: GitFindMatch[] = [];
  for (const hunk of parsed.hunks) {
    for (const line of hunk.lines) {
      if (!line.text.toLowerCase().includes(needle)) continue;
      if (line.kind === "-") matches.push({ side: "deletions", line: line.oldLine!, hunk: hunk.index });
      else matches.push({ side: "additions", line: line.newLine!, hunk: hunk.index });
    }
  }
  return matches;
}

/** Wraps around: past the last match comes the first, before the first the last. */
export function stepGitFind(current: number, count: number, direction: "next" | "previous"): number {
  if (count <= 0) return -1;
  if (current < 0) return direction === "next" ? 0 : count - 1;
  return direction === "next" ? (current + 1) % count : (current - 1 + count) % count;
}

/** `i/n`, `n` before the first step, "No matches"; nothing before a query. */
export function gitFindLabel(query: string, count: number, index: number): string {
  if (!query.trim()) return "";
  if (count === 0) return "No matches";
  return index < 0 ? `${count}` : `${index + 1}/${count}`;
}

/**
 * Where the reader goes when its file left its group (staged, committed, discarded,
 * or changed by the agent): the entry now at the same place in the whole reading
 * order (staged, then changes), so staging the last unstaged file moves on rather
 * than out. Null when nothing is left to read.
 */
export function followGitReviewEntry(order: readonly GitReviewEntry[], lastIndex: number): GitReviewEntry | null {
  if (!order.length) return null;
  return order[Math.min(Math.max(lastIndex, 0), order.length - 1)] ?? null;
}

/** The code text size steps of the reader's display options. */
export const GIT_CODE_TEXT = { min: 10, max: 20, step: 2, initial: 12 } as const;

export function stepGitCodeText(size: number, direction: "smaller" | "larger"): number {
  const next = size + (direction === "larger" ? GIT_CODE_TEXT.step : -GIT_CODE_TEXT.step);
  return Math.min(GIT_CODE_TEXT.max, Math.max(GIT_CODE_TEXT.min, next));
}

export interface GitAskContext {
  /** `app/x.ts:42`, `app/x.ts:12-14`, or the path for the whole file. */
  where: string;
  /** The first lines it is about, as the sheet quotes them. */
  quote: string[];
  /** The message the agent gets for a comment. */
  build(comment: string): string;
}

/**
 * What Ask the agent is about: the selected lines of a hunk (quoted, with their
 * range), else the whole file.
 */
/**
 * The lines an agent is asked about. A whole hunk is sent as it reads, both sides; a range of
 * added lines carries the removed lines between them (the new side alone told the agent the old
 * code was missing: FAILURES R4#5). A range of removed lines stays the old side, as its label says.
 */
function askLines(hunk: GitPatchHunk, side: GitPatchSide, start: number, end: number): GitPatchLine[] {
  const whole = gitHunkSelection(hunk);
  if (whole && whole.side === side && Math.min(start, end) === whole.start && Math.max(start, end) === whole.end) return hunk.lines;
  const picked = gitSelectedLines(hunk, side, start, end);
  if (side !== "additions" || picked.length === 0) return picked;
  return hunk.lines.slice(hunk.lines.indexOf(picked[0]!), hunk.lines.indexOf(picked[picked.length - 1]!) + 1);
}

export function gitAskContext(input: {
  path: string;
  group: GitReviewGroup;
  hunk: GitPatchHunk | null;
  selection: { side: GitPatchSide; start: number; end: number } | null;
}): GitAskContext {
  const { path, group, hunk, selection } = input;
  if (hunk && selection) {
    const { side, start, end } = selection;
    const lines = askLines(hunk, side, start, end);
    const low = Math.min(start, end);
    const high = Math.max(start, end);
    return {
      where: `${path}:${low === high ? low : `${low}-${high}`}`,
      quote: lines.slice(0, 3).map((line) => `${line.kind === " " ? " " : line.kind} ${line.text}`),
      build: (comment) => formatGitAskAgentMessage({ path, side, start, end, lines, comment, staged: group === "staged" }),
    };
  }
  return {
    where: path,
    quote: [],
    build: (comment) => `${group === "staged" ? "In the staged diff" : "In the working-tree diff"}, ${path}:\n\n${comment.trim()}`,
  };
}
