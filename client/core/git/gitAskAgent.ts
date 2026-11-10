import type { GitPatchLine, GitPatchSide } from "./gitDiffHunks";

const MAX_QUOTED_LINES = 12;
const MAX_QUOTED_LINE_LENGTH = 200;

export interface GitAskAgentInput {
  path: string;
  side: GitPatchSide;
  start: number;
  end: number;
  lines: readonly GitPatchLine[];
  comment: string;
  /** The comment is on the staged diff rather than the working tree. */
  staged: boolean;
}

/**
 * The message a review comment sends to the Session's agent: where (file and
 * line, on the current file or on the removed side), the lines quoted from the
 * diff, and what the person wants. The agent can act on it with no more
 * context.
 */
export function formatGitAskAgentMessage(input: GitAskAgentInput): string {
  const low = Math.min(input.start, input.end);
  const high = Math.max(input.start, input.end);
  const range = low === high ? `${low}` : `${low}-${high}`;
  const where =
    input.side === "deletions"
      ? `${input.path}:${range} (removed line${low === high ? "" : "s"}, as it was before the change)`
      : `${input.path}:${range}`;
  const quoted = input.lines.slice(0, MAX_QUOTED_LINES).map((line) => {
    const text = line.text.length > MAX_QUOTED_LINE_LENGTH ? `${line.text.slice(0, MAX_QUOTED_LINE_LENGTH)}…` : line.text;
    return `> ${line.kind === " " ? " " : line.kind} ${text}`;
  });
  if (input.lines.length > MAX_QUOTED_LINES) quoted.push(`> … ${input.lines.length - MAX_QUOTED_LINES} more lines`);
  const scope = input.staged ? "In the staged diff" : "In the working-tree diff";
  return [`${scope}, ${where}:`, ...quoted, "", input.comment.trim()].join("\n");
}
