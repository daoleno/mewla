import { describe, expect, test } from "bun:test";
import { parseResultActions, resultActionLabel, sameResultActions } from "./resultActions";

describe("a result's next steps", () => {
  test("only the kinds the wire names, each with what it needs; none is undefined", () => {
    const parsed = parseResultActions([
      { kind: "pull_request", url: "https://github.com/daoleno/mewla/pull/4242" },
      { kind: "pull_request", url: "javascript:alert(1)" },
      { kind: "git_review", session_id: "s1", cwd: "/repo" },
      { kind: "file", session_id: "s1", cwd: "/repo", path: "docs/NOTES.md" },
      { kind: "file", session_id: "s1", cwd: "/repo" },
      { kind: "brain_folder", path: "worklog/ship" },
      { kind: "next_action", text: "Announce it" },
      "junk",
    ]);
    expect(parsed?.map((a) => resultActionLabel(a).label)).toEqual(["Pull request #4242", "Review changes", "NOTES.md", "Worklog"]);
    expect(parseResultActions([])).toBeUndefined();
    expect(parseResultActions(undefined)).toBeUndefined();
    expect(resultActionLabel({ kind: "pull_request", url: "https://gitlab.com/g/p/-/merge_requests/12" }).label).toBe("Pull request #12");
  });

  test("two lists are the same when every step is", () => {
    const a = parseResultActions([{ kind: "brain_file", path: "x/RESULT.md" }]);
    expect(sameResultActions(a, parseResultActions([{ kind: "brain_file", path: "x/RESULT.md" }]))).toBe(true);
    expect(sameResultActions(a, parseResultActions([{ kind: "brain_file", path: "y/RESULT.md" }]))).toBe(false);
    expect(sameResultActions(undefined, [])).toBe(true);
  });
});
